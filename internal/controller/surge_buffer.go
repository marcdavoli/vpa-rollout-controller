package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"dario.cat/mergo"
	"github.com/influxdata/vpa-rollout-controller/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
)

// Create a "surge buffer" workload resource, which is a copy of the target workload with the resource requests overridden to match the VPA recommendation.
// It uses 'unstructured' to handle different workload types (e.g., Deployment, StatefulSet, etc.) without needing to know the specific type at compile time.
func CreateSurgeBufferWorkload(ctx context.Context, dynamicClient dynamic.Interface, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}) error {
	log := slog.Default()

	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Check that the VPA has a recommendation and store the CPU and memory requests
	if vpa.Status.Recommendation == nil || len(vpa.Status.Recommendation.ContainerRecommendations) == 0 {
		log.Error("VPA recommendation is nil or empty", "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
		return fmt.Errorf("VPA recommendation is nil or empty for VPA %s in namespace %s", vpa.Name, vpa.Namespace)
	}

	// Two-level map to store CPU and memory requests for each container
	vpaRecommendationRequests := make(map[string]map[string]*resource.Quantity)
	vpaRecommendationRequests["cpu"] = make(map[string]*resource.Quantity)
	vpaRecommendationRequests["memory"] = make(map[string]*resource.Quantity)
	for i, container := range vpa.Status.Recommendation.ContainerRecommendations {
		if vpa.Status.Recommendation.ContainerRecommendations[i].Target.Cpu() != nil || vpa.Status.Recommendation.ContainerRecommendations[i].Target.Memory() != nil {
			vpaRecommendationRequests["cpu"][container.ContainerName] = vpa.Status.Recommendation.ContainerRecommendations[i].Target.Cpu()
			vpaRecommendationRequests["memory"][container.ContainerName] = vpa.Status.Recommendation.ContainerRecommendations[i].Target.Memory()
		} else {
			log.Error("VPA recommendation for container is missing CPU or memory target", "ContainerName", container.ContainerName, "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
			return fmt.Errorf("VPA recommendation for container %s is missing CPU or memory target in VPA %s in namespace %s", container.ContainerName, vpa.Name, vpa.Namespace)
		}
	}

	// Determine the number of surge buffer pods
	var surgeBufferReplicas string
	if vpa.Annotations != nil && vpa.Annotations[utils.VPAAnnotationNumberOfSurgeBufferPods] != "" {
		surgeBufferReplicas = vpa.Annotations[utils.VPAAnnotationNumberOfSurgeBufferPods]
	} else {
		surgeBufferReplicas = utils.DefaultSurgeBufferReplicas
	}
	surgeBufferReplicasInt, err := strconv.Atoi(surgeBufferReplicas)
	if err != nil {
		log.Error("Error parsing surge buffer replicas from VPA annotation", "err", err, "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
		return fmt.Errorf("error parsing surge buffer replicas from VPA annotation: %v", err)
	}

	// Make a deep copy of the workload to create the surge buffer pod and override a few fields
	surgeBufferWorkload := runtime.DeepCopyJSON(workload)
	// Explicitly set the contents of the "metadata" fields, since we know exactly what we want to set
	surgeBufferMetadata := make(map[string]interface{})
	surgeBufferMetadata["name"] = fmt.Sprintf("%s-surge-buffer", workloadName)
	surgeBufferMetadata["namespace"] = workloadNamespace
	// Merge existing annotations with surge-buffer annotations
	annotations := make(map[string]interface{})
	err = mergo.Merge(&annotations, surgeBufferWorkload["metadata"].(map[string]interface{})["annotations"].(map[string]interface{}))
	if err != nil {
		log.Error("Error merging annotations for surge buffer workload", "err", err, "WorkloadName", workloadName)
		return fmt.Errorf("error merging annotations for surge buffer workload: %v", err)
	}
	err = mergo.Merge(&annotations, utils.SurgeBufferPodAnnotations)
	if err != nil {
		log.Error("Error merging surge buffer pod annotations", "err", err, "WorkloadName", workloadName)
		return fmt.Errorf("error merging surge buffer pod annotations: %v", err)
	}
	surgeBufferMetadata["annotations"] = annotations
	surgeBufferMetadata["labels"] = surgeBufferWorkload["metadata"].(map[string]interface{})["labels"]
	surgeBufferWorkload["metadata"] = surgeBufferMetadata
	// Only override a few fields in the "spec", since we want to keep the rest of the workload as is
	surgeBufferWorkload["spec"].(map[string]interface{})["replicas"] = surgeBufferReplicasInt
	// Override the resource requests in the podTemplate with the VPA recommendation
	for i, container := range surgeBufferWorkload["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{}) {
		for containerName := range vpaRecommendationRequests["cpu"] {
			if containerName == container.(map[string]interface{})["name"] {
				surgeBufferWorkload["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})[i].(map[string]interface{})["resources"].(map[string]interface{})["requests"].(map[string]interface{})[string(corev1.ResourceCPU)] = vpaRecommendationRequests["cpu"][containerName].String()
				surgeBufferWorkload["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})[i].(map[string]interface{})["resources"].(map[string]interface{})["requests"].(map[string]interface{})[string(corev1.ResourceMemory)] = vpaRecommendationRequests["memory"][containerName].String()
				surgeBufferWorkload["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})[i].(map[string]interface{})["resources"].(map[string]interface{})["limits"].(map[string]interface{})[string(corev1.ResourceMemory)] = vpaRecommendationRequests["memory"][containerName].String()
			}
		}

	}
	// Remove the "status" field from the surge buffer workload, since we don't want to set it
	delete(surgeBufferWorkload, "status")

	// Create the surge buffer workload using the typed client
	surgeBufferWorkloadResource := &unstructured.Unstructured{Object: surgeBufferWorkload}
	gvk := surgeBufferWorkloadResource.GroupVersionKind()
	gvr := schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: strings.ToLower(gvk.Kind) + "s",
	}
	_, err = dynamicClient.Resource(gvr).Namespace(workloadNamespace.(string)).Create(ctx, surgeBufferWorkloadResource, metav1.CreateOptions{})
	if err != nil {
		log.Error("Error creating surge buffer workload", "err", err, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
		return fmt.Errorf("error creating surge buffer workload: %v", err)
	}

	log.Info("Created surge buffer pod from workload template", "WorkloadName", workloadName, "SurgeWorkload", surgeBufferWorkload)

	return nil

}

// Delete the surge buffer workload resource created for the VPA target workload.
// This is used to clean up the surge buffer workload after the rollout is complete.
func DeleteSurgeBufferWorkload(ctx context.Context, dynamicClient dynamic.Interface, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}) error {
	log := slog.Default()

	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	surgeBufferWorkloadName := fmt.Sprintf("%s-surge-buffer", workloadName)

	gvr := schema.GroupVersionResource{
		Group:    strings.SplitN(vpa.Spec.TargetRef.APIVersion, "/", 2)[0],
		Version:  strings.SplitN(vpa.Spec.TargetRef.APIVersion, "/", 2)[1],
		Resource: strings.ToLower(vpa.Spec.TargetRef.Kind + "s"),
	}

	// Delete the surge buffer workload
	err := dynamicClient.Resource(gvr).Namespace(workloadNamespace.(string)).Delete(ctx, surgeBufferWorkloadName, metav1.DeleteOptions{})
	if err != nil {
		log.Error("Error deleting surge buffer workload", "err", err, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
		return fmt.Errorf("error deleting surge buffer workload: %v", err)
	}

	log.Info("Deleted surge buffer workload", "SurgeBufferWorkloadName", surgeBufferWorkloadName, "WorkloadName", workloadName)

	return nil
}
