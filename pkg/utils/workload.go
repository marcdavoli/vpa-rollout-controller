package utils

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Check if the VPA has the "enabled" annotation set to "true" and that the VPA's updateMode is set to 'Initial'
func VPAIsEligible(ctx context.Context, vpa *v1.VerticalPodAutoscaler) bool {

	log := log.FromContext(ctx)
	// Check if the VPA updateMode is set to Initial
	if vpa.Spec.UpdatePolicy.UpdateMode != nil && *vpa.Spec.UpdatePolicy.UpdateMode == v1.UpdateModeInitial {
		// Check if the VPA has the annotation "vpa-rollout.influxdata.io/enabled" set to "true"
		if vpa.Annotations != nil && vpa.Annotations[VPAAnnotationEnabled] == "true" {
			return true
		} else {
			log.V(1).Info("VPA is not eligible for processing", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name, "Reason", "Annotation 'vpa-rollout.influxdata.io/enabled' not set to 'true'")

		}
	} else {
		log.V(1).Info("VPA is not eligible for processing", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name, "Reason", "UpdateMode is not set to 'Initial'")
	}
	return false
}

// Get the target workload from the VPA spec
func GetTargetWorkload(ctx context.Context, vpa *v1.VerticalPodAutoscaler, dynamicClient dynamic.Interface) (map[string]interface{}, error) {

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

// Get the VPA's target workload resource's pods using selector labels
func getTargetWorkloadPods(ctx context.Context, workload map[string]interface{}, clientset kubernetes.Interface) (*corev1.PodList, error) {

	log := log.FromContext(ctx)
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Get the selector labels from the workload
	selectorLabels, _, err := unstructured.NestedStringMap(workload, "spec", "selector", "matchLabels")
	if err != nil {
		log.Error(err, "Error getting selector labels from workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return nil, err
	}

	// Get the pods using the selector labels and typedClient
	podList, err := clientset.CoreV1().Pods(workloadNamespace.(string)).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(selectorLabels).String(),
	})
	if err != nil {
		log.Error(err, "Error getting pods for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return nil, err
	}

	return podList, nil
}

// Perform multiple checks to ensure the workload's pods are healthy and ready for a rollout
func workloadPodsAreHealthy(ctx context.Context, workload map[string]interface{}, clientset kubernetes.Interface) (bool, error) {

	log := log.FromContext(ctx)
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Get the list of pods for the target workload
	podList, err := getTargetWorkloadPods(ctx, workload, clientset)
	if err != nil {
		log.Error(err, "Error getting pods for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}

	// Ensure there are pods before proceeding
	if len(podList.Items) == 0 {
		log.V(1).Info("No pods found for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, nil
	}

	// Store the resources block for each container across all the pods
	containerResourcesRequestsMap := make(map[string]corev1.ResourceList)

	for _, pod := range podList.Items {
		// If any of the pods are not in Running state, do not trigger a rollout
		if pod.Status.Phase != corev1.PodRunning {
			log.V(1).Info("At least one of the target workload's Pods is not in Running state", "podName", pod.Name, "podNamespace", pod.Namespace, "podStatus", pod.Status.Phase, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return false, nil
		}
		// If one of the containers in the pod is not Ready, do not trigger a rollout
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				log.V(1).Info("At least one of the target workload's Pods's containers is not Ready", "podName", pod.Name, "podNamespace", pod.Namespace, "containerName", containerStatus.Name, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
				return false, nil
			}
		}

		// If a given container doesn't have the same resource requests across all the pods, do not trigger a rollout
		for _, container := range pod.Spec.Containers {
			containerResourcesRequests := container.Resources.Requests
			if containerResourcesRequestsMap[container.Name] != nil && !reflect.DeepEqual(containerResourcesRequestsMap[container.Name], containerResourcesRequests) {
				log.V(1).Info("At least one of the target workload's Pods's containers has different resource requests", "podName", pod.Name, "podNamespace", pod.Namespace, "containerName", container.Name, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
				return false, nil
			}
			containerResourcesRequestsMap[container.Name] = containerResourcesRequests
		}

	}

	return true, nil
}
