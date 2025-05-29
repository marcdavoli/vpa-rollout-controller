package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Check if a rollout is needed based on the VPA recommendation and the workload's pods' current resource requests
func RolloutIsNeeded(ctx context.Context, clientset kubernetes.Interface, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, diffPercentTrigger int) (bool, error) {

	log := slog.Default()
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Ensure the workload's pods are healthy before proceeding
	healthy, err := workloadPodsAreHealthy(ctx, workload, clientset)
	if err != nil {
		log.Error("Error checking workload pods health", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}
	if !healthy {
		log.Info("Workload pods are not healthy, skipping rollout", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, nil
	}

	// Override the diffPercentTrigger if the VPA annotation is specified
	var effectiveDiffPercentTrigger int
	if vpa.Annotations != nil && vpa.Annotations[utils.VPAAnnotationDiffPercentTrigger] != "" {
		overridenDiffPercentTrigger, err := strconv.Atoi(vpa.Annotations[utils.VPAAnnotationDiffPercentTrigger])
		if err != nil {
			log.Error("Error parsing diffPercentTrigger from VPA annotation", "err", err, "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
			return false, err
		}
		effectiveDiffPercentTrigger = overridenDiffPercentTrigger

	} else {
		effectiveDiffPercentTrigger = diffPercentTrigger
	}

	if vpa.Status.Recommendation != nil && vpa.Status.Recommendation.ContainerRecommendations != nil {
		for _, recommendation := range vpa.Status.Recommendation.ContainerRecommendations {
			if recommendation.Target != nil {
				log.Debug("Processing VPA Recommendation", "ContainerName", recommendation.ContainerName, "ContainerTargetCPU", recommendation.Target.Cpu(), "ContainerTargetMemory", recommendation.Target.Memory())
				if recommendation.Target.Cpu() != nil && recommendation.Target.Memory() != nil {

					// Get the current CPU and Memory request from the target workload
					podList, err := getTargetWorkloadPods(ctx, workload, clientset)
					if err != nil {
						log.Error("Error getting pods for workload", "error", err.Error(), "workloadName", workloadName, "workloadNamespace", workloadNamespace)
						return false, err
					}
					// We can pick the first pod in the list, since we've previously verified that the 'resources' block is the same across all pods
					pod := podList.Items[0]
					var containerCPU, containerMemory *resource.Quantity
					for _, container := range pod.Spec.Containers {
						if container.Name == recommendation.ContainerName {
							containerResources := container.Resources
							containerCPU = containerResources.Requests.Cpu()
							containerMemory = containerResources.Requests.Memory()
							log.Debug("Container Spec values", "vpaName", vpa.Name, "vpaNamespace", vpa.Namespace, "podName", pod.Name, "ContainerName", container.Name, "ContainerCPURequests", containerCPU.String(), "ContainerMemoryRequests", containerMemory.String())
						}
					}

					// Get the target CPU and Memory request from the VPA recommendation
					vpaTargetCpuQuantity, _ := resource.ParseQuantity(recommendation.Target.Cpu().String())
					vpaTargetMemoryQuantity, _ := resource.ParseQuantity(recommendation.Target.Memory().String())
					log.Debug("VPA Status values", "VpaTargetCpuQuantity", vpaTargetCpuQuantity.String(), "VpaTargetMemoryQuantity", vpaTargetMemoryQuantity.String())
					// Calculate the difference between current and target CPU and Memory requests
					cpuDiff := math.Abs(containerCPU.AsApproximateFloat64() - vpaTargetCpuQuantity.AsApproximateFloat64())
					cpuDiffPercent := cpuDiff / vpaTargetCpuQuantity.AsApproximateFloat64() * 100
					memoryDiff := math.Abs(containerMemory.AsApproximateFloat64() - vpaTargetMemoryQuantity.AsApproximateFloat64())
					memoryDiffPercent := memoryDiff / vpaTargetMemoryQuantity.AsApproximateFloat64() * 100
					log.Debug("Calculated diff between VPA Resource Target and Workload Resources", "CPUDiff", cpuDiff, "CPUDiffPercent", cpuDiffPercent, "MemoryDiff", memoryDiff, "MemoryDiffPercent", memoryDiffPercent)

					// If difference between current and target CPU or Memory is greater than the threshold, trigger a rollout
					if cpuDiffPercent > float64(effectiveDiffPercentTrigger) || memoryDiffPercent > float64(effectiveDiffPercentTrigger) {
						log.Info("Rollout needed for VPA Target Workload", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name, "cpuDiffPercent", cpuDiffPercent, "memoryDiffPercent", memoryDiffPercent, "diffPercentTrigger", effectiveDiffPercentTrigger)
						return true, nil
					} else {
						log.Info("No rollout needed for VPA Target Workload", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)
						return false, nil
					}
				}
			}
		}
	} else {
		log.Debug("No recommendation for VPA", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)
		return false, nil
	}
	return false, fmt.Errorf("error verifying if rollout is needed for VPA %s", vpa.Name)
}

// Get the current rollout status of the VPA from its annotation
func GetRolloutStatus(ctx context.Context, vpa v1.VerticalPodAutoscaler) string {
	if vpa.Annotations == nil {
		return ""
	}
	return vpa.Annotations[utils.VPAAnnotationRolloutStatus]
}

// Set the VPA annotation that reflects the latest status of a rollout
func SetRolloutStatus(ctx context.Context, vpa v1.VerticalPodAutoscaler, dynamicClient dynamic.Interface, patchOperationFieldManager string, status string) error {
	log := slog.Default()

	patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`, utils.VPAAnnotationRolloutStatus, status)
	gvr := schema.GroupVersionResource{
		Group:    "autoscaling.k8s.io",
		Version:  "v1",
		Resource: "verticalpodautoscalers",
	}
	_, err := dynamicClient.Resource(gvr).Namespace(vpa.Namespace).Patch(ctx, vpa.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{FieldManager: patchOperationFieldManager})
	if err != nil {
		log.Error("Error setting rollout status for workload", "err", err, "vpaName", vpa.Name, "vpaNamespace", vpa.Namespace, "status", status)
		return fmt.Errorf("error setting rollout status to '%s' for workload %s: %v", status, vpa.Name, err)
	}

	log.Info("Set rollout status for workload", "VPA", vpa.Name, "VPA Namespace", vpa.Namespace, "Status", status)

	return nil
}

// Check if the workload pods are ready and restarted since the last rollout
func RolloutIsCompleted(ctx context.Context, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, clientset kubernetes.Interface) (bool, error) {
	log := slog.Default()
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Check if the workload's pods are healthy
	healthy, err := workloadPodsAreHealthy(ctx, workload, clientset)
	if err != nil {
		log.Error("Error checking workload pods health", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}
	if !healthy {
		log.Info("Workload pods are not healthy, rollout is not complete", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, nil
	}

	// Check if the pods' age is less than the time since the last rollout
	podList, err := getTargetWorkloadPods(ctx, workload, clientset)
	if err != nil {
		log.Error("Error getting pods for workload", "error", err.Error(), "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}
	for _, pod := range podList.Items {
		var lastRolloutTimeStr string
		if templateAnnotations, ok := workload["spec"].(map[string]interface{})["template"].(map[string]interface{})["metadata"].(map[string]interface{})["annotations"].(map[string]interface{}); ok {
			if restartedAt, ok := templateAnnotations["kubectl.kubernetes.io/restartedAt"]; ok && restartedAt != nil {
				lastRolloutTimeStr, _ = restartedAt.(string)
			}
		}
		if lastRolloutTimeStr == "" {
			log.Info("No last rollout time found in workload template annotations, assuming rollout is not complete", "podName", pod.Name, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return false, nil
		}
		lastRolloutTime, err := time.Parse(time.RFC3339, lastRolloutTimeStr)
		if err != nil {
			log.Error("Error parsing last rollout time from pod annotations", "err", err, "podName", pod.Name, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return false, err
		}
		podAge := time.Since(pod.GetCreationTimestamp().Time)
		if podAge < time.Since(lastRolloutTime) {
			log.Info("Pod has been restarted since the last rollout", "podName", pod.Name, "podStartTime", pod.Status.StartTime, "lastRolloutTime", lastRolloutTime, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return true, nil
		} else {
			log.Info("Pod has not been restarted since the last rollout", "podName", pod.Name, "podStartTime", pod.Status.StartTime, "lastRolloutTime", lastRolloutTime, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return false, nil
		}
	}

	log.Info("Rollout is still in progress for VPA", "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
	return false, nil
}
