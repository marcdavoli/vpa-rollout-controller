package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
)

// Triggers the rollout process for a workload, including creating a surge buffer workload if enabled in the VPA annotations.
func TriggerRollout(ctx context.Context, workload map[string]interface{}, vpa v1.VerticalPodAutoscaler, dynamicClient dynamic.Interface, patchOperationFieldManager string) error {

	log := slog.Default()

	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	log.Info("Triggering rollout for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)

	// If the VPA has the surge buffer enabled, create the surge buffer workload
	if vpa.Annotations != nil && vpa.Annotations[utils.VPAAnnotationSurgeBufferEnabled] == "true" {
		err := CreateSurgeBufferWorkload(ctx, dynamicClient, vpa, workload)
		if err != nil {
			log.Error("Error creating surge buffer workload", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
			return fmt.Errorf("error creating surge buffer workload for %s: %v", workloadName, err)
		}
		log.Info("Surge buffer workload created successfully", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		SetRolloutStatus(ctx, vpa, dynamicClient, patchOperationFieldManager, "pending")
		log.Info("Set the VPA rollout status annotation to 'pending'", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return nil
	}

	err := triggerRolloutRestart(ctx, workload, dynamicClient, patchOperationFieldManager)
	if err != nil {
		log.Error("Error triggering rollout for workload", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return fmt.Errorf("error triggering rollout for workload %s: %v", workloadName, err)
	}

	return nil
}

func TriggerPendingRollout(ctx context.Context, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, dynamicClient dynamic.Interface, patchOperationFieldManager string) error {
	log := slog.Default()

	// Trigger the rollout restart
	err := triggerRolloutRestart(ctx, workload, dynamicClient, patchOperationFieldManager)
	if err != nil {
		log.Error("Error triggering rollout restart for workload", "err", err, "workloadName", workload["metadata"].(map[string]interface{})["name"], "workloadNamespace", workload["metadata"].(map[string]interface{})["namespace"])
		return fmt.Errorf("error triggering rollout restart for workload %s: %v", workload["metadata"].(map[string]interface{})["name"], err)
	}
	// Set the VPA annotation to indicate that a rollout is pending
	patchData := fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`, utils.VPAAnnotationRolloutStatus, "in-progress")
	gvr := schema.GroupVersionResource{
		Group:    "autoscaling.k8s.io",
		Version:  "v1",
		Resource: "verticalpodautoscalers",
	}
	_, err = dynamicClient.Resource(gvr).Namespace(vpa.Namespace).Patch(ctx, vpa.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{FieldManager: patchOperationFieldManager})
	if err != nil {
		log.Error("Error triggering pending rollout for workload", "err", err, "vpaName", vpa.Name, "vpaNamespace", vpa.Namespace)
		return fmt.Errorf("error triggering pending rollout for workload %s: %v", vpa.Name, err)
	}

	log.Info("Triggered pending rollout for workload", "VPA", vpa.Name, "VPA Namespace", vpa.Namespace)

	return nil
}

// Patches the workload resource to trigger a rollout using the annotation 'kubectl.kubernetes.io/restartedAt'
func triggerRolloutRestart(ctx context.Context, workload map[string]interface{}, dynamicClient dynamic.Interface, patchOperationFieldManager string) error {

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
	}

	log.Info("Rollout triggered successfully", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "timestamp", currentTime)
	return nil
}
