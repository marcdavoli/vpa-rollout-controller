package controller

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
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
				log.Info("Processing VPA Recommendation", "ContainerName", recommendation.ContainerName, "ContainerTargetCPU", recommendation.Target.Cpu(), "ContainerTargetMemory", recommendation.Target.Memory())
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
							log.Debug("Found container in pod spec", "ContainerName", container.Name)
							containerResources := container.Resources
							containerCPU = containerResources.Requests.Cpu()
							containerMemory = containerResources.Requests.Memory()
							log.Debug("Container Spec values", "ContainerCPURequests", containerCPU.String(), "ContainerMemoryRequests", containerMemory.String())
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

// Patches the workload resource to trigger a rollout using the annotation 'kubectl.kubernetes.io/restartedAt'
func TriggerRollout(ctx context.Context, workload map[string]interface{}, dynamicClient dynamic.Interface, patchOperationFieldManager string) error {

	log := slog.Default()

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
		log.Error("Error triggering rollout on workload", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace, "Group", gvr.Group, "Version", gvr.Version, "Resource", gvr.Resource, "patchData", patchData)
		return err
	} else {
		log.Info("Rollout triggered successfully", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "timestamp", currentTime)
	}
	return nil
}
