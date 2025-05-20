package utils

import (
	"context"
	"log/slog"
	"time"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/kubernetes"
)

// Check if the cooldown period has elapsed, to avoid rolling too frequently
func CooldownHasElapsed(ctx context.Context, clientset kubernetes.Interface, vpa v1.VerticalPodAutoscaler, workload map[string]interface{}, cooldownPeriodDuration time.Duration) (bool, error) {

	log := slog.Default()
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Override the cooldown period duration if the VPA annotation for this purpose is specified
	var effectiveCooldownPeriodDuration time.Duration
	if vpa.Annotations != nil && vpa.Annotations[utils.VPAAnnotationCooldownPeriod] != "" {
		overridenCooldownPeriodDuration, err := time.ParseDuration(vpa.Annotations[utils.VPAAnnotationCooldownPeriod])
		if err != nil {
			log.Error("Error parsing cooldown period duration from VPA annotation", "err", err, "VPAName", vpa.Name, "VPANameSpace", vpa.Namespace)
			return false, err
		}
		effectiveCooldownPeriodDuration = overridenCooldownPeriodDuration
	} else {
		effectiveCooldownPeriodDuration = cooldownPeriodDuration
	}

	log.Info("Effective cooldown period duration", "vpa", vpa.Name, "vpaNamespace", vpa.Namespace, "cooldownPeriodDuration", effectiveCooldownPeriodDuration)

	// Check if the annotation 'kubectl.kubernetes.io/restartedAt' is present in the workload
	timestamp, timestampFound, err := unstructured.NestedString(workload, "spec", "template", "metadata", "annotations", "kubectl.kubernetes.io/restartedAt")
	if err != nil {
		log.Error("Error getting timestamp", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}

	// Check that the workload's pods' age is greater than the cooldown period
	podList, err := getTargetWorkloadPods(ctx, workload, clientset)
	if err != nil {
		log.Error("Error getting pods for workload", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, err
	}
	if len(podList.Items) == 0 {
		log.Info("No pods found for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return false, nil
	}
	for _, pod := range podList.Items {
		podAge := time.Since(pod.GetCreationTimestamp().Time)
		log.Info("Pod age", "podName", pod.Name, "podNamespace", pod.Namespace, "podAge", podAge.Round(time.Second))
		if podAge < effectiveCooldownPeriodDuration {
			log.Info("Workload's Pod age is less than cooldown period", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "podName", pod.Name, "podNamespace", pod.Namespace, "podAge", podAge.Round(time.Second), "cooldownPeriodDuration", effectiveCooldownPeriodDuration)
			return false, nil
		}
	}

	if timestampFound {
		log.Info("Found timestamp", "WorkloadName", workloadName, "workloadNamespace", workloadNamespace, "lastRestartedAt", timestamp)
		lastRestartedAt, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			log.Error("Error parsing timestamp for Workload", "err", err, "workloadName", workloadName, "workloadNamespace", workloadNamespace, "timestamp", timestamp)
			return false, err
		}
		if time.Since(lastRestartedAt) > effectiveCooldownPeriodDuration {
			log.Info("Cooldown period has elapsed for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "elapsedTime", time.Since(lastRestartedAt).Round(time.Second), "cooldownPeriodDuration", effectiveCooldownPeriodDuration)
			return true, nil
		} else {
			log.Info("Cooldown period has not elapsed for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace, "elapsedTime", time.Since(lastRestartedAt).Round(time.Second), "cooldownPeriodDuration", effectiveCooldownPeriodDuration)
		}

	} else {
		log.Debug("No timestamp found for workload", "workloadName", workloadName, "workloadNamespace", workloadNamespace)
		return true, nil
	}
	return false, nil
}
