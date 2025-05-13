package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
)

func rolloutIsNeeded(ctx context.Context, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}) bool {

	if vpa.Status.Recommendation != nil {
		for _, container := range vpa.Status.Recommendation.ContainerRecommendations {
			if container.Target != nil {
				fmt.Printf("Container %s: CPU: %s, Memory: %s\n", container.ContainerName, container.Target.Cpu(), container.Target.Memory())
				if container.Target.Cpu() != nil && container.Target.Memory() != nil {

					// get the current CPU and Memory request from the target workload
					cpuRequests, _, _ := unstructured.NestedString(workload, "spec", "template", "spec", "containers", "0", "resources", "requests", "cpu")
					memoryRequests, _, _ := unstructured.NestedString(workload, "spec", "template", "spec", "containers", "0", "resources", "requests", "memory")
					fmt.Printf("Current CPU Requests: %s, Memory Requests: %s\n", cpuRequests, memoryRequests)

					cpuRequestsQuantity, _ := resource.ParseQuantity(cpuRequests)
					targetCpuQuantity, _ := resource.ParseQuantity(container.Target.Cpu().String())
					memoryRequestsQuantity, _ := resource.ParseQuantity(memoryRequests)
					targetMemoryQuantity, _ := resource.ParseQuantity(container.Target.Memory().String())
					fmt.Printf("Target CPU Requests: %s, Target Memory: %s\n", targetCpuQuantity.String(), targetMemoryQuantity.String())

					// Calculate the difference between current and target CPU and Memory requests
					cpuDiff := cpuRequestsQuantity.AsApproximateFloat64() - targetCpuQuantity.AsApproximateFloat64()
					cpuDiffPercent := cpuDiff / targetCpuQuantity.AsApproximateFloat64() * 100
					memoryDiff := memoryRequestsQuantity.AsApproximateFloat64() - targetMemoryQuantity.AsApproximateFloat64()
					memoryDiffPercent := memoryDiff / targetMemoryQuantity.AsApproximateFloat64() * 100
					fmt.Printf("CPU Diff: %f %f%%, Memory Diff: %f %f%%\n", cpuDiff, cpuDiffPercent, memoryDiff, memoryDiffPercent)

					// If difference between current and target CPU or Memory is greater than 10%
					if cpuDiffPercent > diffPercentTrigger || memoryDiffPercent > diffPercentTrigger {
						fmt.Printf("Rollout needed for VPA %s\n", vpa.Name)
						return true
					}
				}
			}
		}
	} else {
		fmt.Printf("No recommendation for VPA %s\n", vpa.Name)
	}
	return false
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

// Check if the cooldown period has elapsed
func cooldownHasElapsed(ctx context.Context, workload map[string]interface{}) bool {

	workloadName := workload["metadata"].(map[string]interface{})["name"]

	timestamp, timestampFound, err := unstructured.NestedString(workload, "metadata", "annotations", "kubectl.kubernetes.io/restartedAt")
	if err != nil {
		fmt.Printf("Error getting timestamp: %v\n", err)
		return false
	}

	if timestampFound {
		fmt.Printf("Workload %s, Last Restarted At: %s\n", workloadName, timestamp)
		lastRestartedAt, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			fmt.Printf("Error parsing timestamp for Workload %s: %v\n", workloadName, err)
			return false
		}
		if time.Since(lastRestartedAt) > cooldownPeriod {
			fmt.Printf("Cooldown period has elapsed for workload %s, it has been %s\n", workloadName, time.Since(lastRestartedAt))
			return true
		} else {
			fmt.Printf("Cooldown period has not elapsed for workload %s, it has been %s\n", workloadName, time.Since(lastRestartedAt))
		}

	} else {
		fmt.Printf("No timestamp found for workload %s\n", workloadName)
		return true
	}
	return false
}

// Patches the workload to trigger a rollout using the annotation 'kubectl.kubernetes.io/restartedAt'
func triggerRollout(ctx context.Context, workload map[string]interface{}, dynamicClient dynamic.Interface) {
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	currentTime := time.Now().Format(time.RFC3339)

	patchData := fmt.Sprintf(`{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}`, currentTime)
	gvr := schema.GroupVersionResource{
		Group:    strings.SplitN(workload["apiVersion"].(string), "/", 2)[0],
		Version:  strings.SplitN(workload["apiVersion"].(string), "/", 2)[1],
		Resource: strings.ToLower(workload["kind"].(string) + "s"),
	}
	_, err := dynamicClient.Resource(gvr).Namespace(workloadNamespace.(string)).Patch(ctx, workloadName.(string), types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
	if err != nil {
		fmt.Printf("Error triggering rollout on workload %s: %v\n", workloadName, err)
	} else {
		fmt.Printf("Rollout triggered for workload %s\n", workloadName)
	}

}
