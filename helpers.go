package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Check if a rollout is needed based on the VPA recommendation and the workload's current resource requests
func rolloutIsNeeded(ctx context.Context, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, diffPercentTrigger int) (bool, error) {

	log := log.FromContext(ctx)

	// Override the diffPercentTrigger if the VPA annotation is specified
	var effectiveDiffPercentTrigger int
	if vpa.Annotations != nil && vpa.Annotations[vpaAnnotationDiffPercentTrigger] != "" {
		overridenDiffPercentTrigger, err := strconv.Atoi(vpa.Annotations[vpaAnnotationDiffPercentTrigger])
		if err != nil {
			log.Error(err, "Error parsing diffPercentTrigger from VPA annotation", "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
			return false, err
		}
		effectiveDiffPercentTrigger = overridenDiffPercentTrigger

	} else {
		effectiveDiffPercentTrigger = diffPercentTrigger
	}

	if vpa.Status.Recommendation != nil {
		for _, recommendation := range vpa.Status.Recommendation.ContainerRecommendations {
			if recommendation.Target != nil {
				log.V(1).Info("Processing VPA Recommendation", "ContainerName", recommendation.ContainerName, "ContainerTargetCPU", recommendation.Target.Cpu(), "ContainerTargetMemory", recommendation.Target.Memory())
				if recommendation.Target.Cpu() != nil && recommendation.Target.Memory() != nil {

					// Get the current CPU and Memory request from the target workload
					workloadContainers, _, _ := unstructured.NestedSlice(workload, "spec", "template", "spec", "containers")
					for c := range workloadContainers {
						if workloadContainers[c].(map[string]interface{})["name"] == recommendation.ContainerName {
							containerResources, found, err := unstructured.NestedMap(workloadContainers[c].(map[string]interface{}), "resources")
							if err != nil {
								log.Error(err, "Error getting container resources")
								continue
							}
							if !found {
								log.V(1).Info("No resources found for container", "containerName", recommendation.ContainerName)
								continue
							}
							workloadCPURequests, _, _ := unstructured.NestedString(containerResources, "requests", "cpu")
							workloadMemoryRequests, _, _ := unstructured.NestedString(containerResources, "requests", "memory")
							workloadCPURequestsQuantity, _ := resource.ParseQuantity(workloadCPURequests)
							workloadMemoryRequestsQuantity, _ := resource.ParseQuantity(workloadMemoryRequests)
							log.V(1).Info("Workload Spec values", "WorkloadCPURequests", workloadCPURequests, "WorkloadMemoryRequests", workloadMemoryRequests)

							// Get the target CPU and Memory request from the VPA recommendation
							vpaTargetCpuQuantity, _ := resource.ParseQuantity(recommendation.Target.Cpu().String())
							vpaTargetMemoryQuantity, _ := resource.ParseQuantity(recommendation.Target.Memory().String())
							log.V(1).Info("VPA Status values", "VpaTargetCpuQuantity", vpaTargetCpuQuantity.String(), "VpaTargetMemoryQuantity", vpaTargetMemoryQuantity.String())
							// Calculate the difference between current and target CPU and Memory requests
							cpuDiff := workloadCPURequestsQuantity.AsApproximateFloat64() - vpaTargetCpuQuantity.AsApproximateFloat64()
							cpuDiffPercent := cpuDiff / vpaTargetCpuQuantity.AsApproximateFloat64() * 100
							memoryDiff := workloadMemoryRequestsQuantity.AsApproximateFloat64() - vpaTargetMemoryQuantity.AsApproximateFloat64()
							memoryDiffPercent := memoryDiff / vpaTargetMemoryQuantity.AsApproximateFloat64() * 100
							log.V(1).Info("Calculated diff between VPA Resource Target and Workload Resources", "CPUDiff", cpuDiff, "CPUDiffPercent", cpuDiffPercent, "MemoryDiff", memoryDiff, "MemoryDiffPercent", memoryDiffPercent)

							// If difference between current and target CPU or Memory is greater than the threshold, trigger a rollout
							if cpuDiffPercent > float64(effectiveDiffPercentTrigger) || memoryDiffPercent > float64(effectiveDiffPercentTrigger) {
								log.V(1).Info("Rollout needed for VPA Target Workload", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)
								return true, nil
							}
						}
					}
				}
			}
		}
	} else {
		log.V(1).Info("No recommendation for VPA", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)
		return false, nil
	}
	return false, fmt.Errorf("error verifying if rollout is needed for VPA %s", vpa.Name)
}

// Get the target workload from the VPA spec
func getTargetWorkload(ctx context.Context, vpa v1.VerticalPodAutoscaler, dynamicClient dynamic.Interface) (map[string]interface{}, error) {

	vpaTargetGroupVersionResource := schema.GroupVersionResource{
		Group:    strings.SplitN(vpa.Spec.TargetRef.APIVersion, "/", 2)[0],
		Version:  strings.SplitN(vpa.Spec.TargetRef.APIVersion, "/", 2)[1],
		Resource: strings.ToLower(vpa.Spec.TargetRef.Kind + "s"),
	}

	unstructuredObj, err := dynamicClient.Resource(vpaTargetGroupVersionResource).Namespace(vpa.Namespace).Get(ctx, vpa.Spec.TargetRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting target workload: %v", err)
	}

	return unstructuredObj.UnstructuredContent(), nil
}

// Check if the cooldown period has elapsed, to avoid rolling too frequently
func cooldownHasElapsed(ctx context.Context, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, cooldownPeriodDuration time.Duration) (bool, error) {

	log := log.FromContext(ctx)
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Override the cooldown period duration if the VPA annotation is specified
	var effectiveCooldownPeriodDuration time.Duration
	if vpa.Annotations != nil && vpa.Annotations[vpaAnnotationCooldownPeriod] != "" {
		overridenCooldownPeriodDuration, err := time.ParseDuration(vpa.Annotations[vpaAnnotationCooldownPeriod])
		if err != nil {
			log.Error(err, "Error parsing cooldown period duration from VPA annotation", "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
			return false, err
		}
		effectiveCooldownPeriodDuration = overridenCooldownPeriodDuration
	} else {
		effectiveCooldownPeriodDuration = cooldownPeriodDuration
	}

	// Check if the annotation 'kubectl.kubernetes.io/restartedAt' is present in the workload
	timestamp, timestampFound, err := unstructured.NestedString(workload, "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt")
	if err != nil {
		log.Error(err, "Error getting timestamp", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}

	if timestampFound {
		log.V(1).Info("Found timestamp", "WorkloadName", workloadName, "workloadNamespace", workloadNamespace, "lastRestartedAt", timestamp)
		lastRestartedAt, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			log.Error(err, "Error parsing timestamp for Workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "timestamp", timestamp)
			return false, err
		}
		if time.Since(lastRestartedAt) > effectiveCooldownPeriodDuration {
			log.V(1).Info("Cooldown period has elapsed for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "elapsedTime", time.Since(lastRestartedAt).Round(time.Second), "cooldownPeriodDuration", effectiveCooldownPeriodDuration)
			return true, nil
		} else {
			log.V(1).Info("Cooldown period has not elapsed for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "elapsedTime", time.Since(lastRestartedAt).Round(time.Second), "cooldownPeriodDuration", effectiveCooldownPeriodDuration)
		}

	} else {
		log.V(1).Info("No timestamp found for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return true, nil
	}
	return false, nil
}

// Patches the workload resource to trigger a rollout using the annotation 'kubectl.kubernetes.io/restartedAt'
func triggerRollout(ctx context.Context, workload map[string]interface{}, dynamicClient dynamic.Interface, patchOperationFieldManager string) error {

	log := log.FromContext(ctx)

	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	currentTime := time.Now().Format(time.RFC3339)
	patchData := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`, currentTime)
	gvr := schema.GroupVersionResource{
		Group:    strings.SplitN(workload["apiVersion"].(string), "/", 2)[0],
		Version:  strings.SplitN(workload["apiVersion"].(string), "/", 2)[1],
		Resource: strings.ToLower(workload["kind"].(string) + "s"),
	}
	_, err := dynamicClient.Resource(gvr).Namespace(workloadNamespace.(string)).Patch(ctx, workloadName.(string), types.MergePatchType, []byte(patchData), metav1.PatchOptions{FieldManager: patchOperationFieldManager})
	if err != nil {
		log.Error(err, "Error triggering rollout on workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "Group", gvr.Group, "Version", gvr.Version, "Resource", gvr.Resource, "patchData", patchData)
		return err
	} else {
		log.V(1).Info("Rollout triggered successfully", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "timestamp", currentTime)
	}
	return nil
}

// Check if the VPA has the "enabled" annotation set to "true" and that the VPA's updateMode is set to 'Initial'
func vpaIsEligible(ctx context.Context, vpa v1.VerticalPodAutoscaler) bool {

	log := log.FromContext(ctx)
	// Check if the VPA updateMode is set to Initial
	if vpa.Spec.UpdatePolicy.UpdateMode != nil && *vpa.Spec.UpdatePolicy.UpdateMode == v1.UpdateModeInitial {
		// Check if the VPA has the annotation "vpa-rollout.influxdata.io/enabled" set to "true"
		if vpa.Annotations != nil && vpa.Annotations[vpaAnnotationEnabled] == "true" {
			return true
		} else {
			log.V(1).Info("VPA is not eligible for processing", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name, "Reason", "Annotation 'vpa-rollout.influxdata.io/enabled' not set to 'true'")

		}
	} else {
		log.V(1).Info("VPA is not eligible for processing", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name, "Reason", "UpdateMode is not set to 'Initial'")
	}
	return false
}
