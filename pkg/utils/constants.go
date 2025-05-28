package utils

const (
	// Enables the vpa-rollout controller to operate on the VPA
	VPAAnnotationEnabled = "vpa-rollout.influxdata.io/enabled"

	// Override the cooldown period between rollouts for a specific VPA
	VPAAnnotationCooldownPeriod = "vpa-rollout.influxdata.io/cooldown-period"

	// Override the percentage difference that will trigger a rollout for the VPA's target workload
	VPAAnnotationDiffPercentTrigger = "vpa-rollout.influxdata.io/diff-percent-trigger"

	// Enables the surge buffer feature for the VPA's target workload
	// This will create a "surge buffer" workload resource that is a copy of the target workload with the resource requests overridden to match the VPA recommendation.
	VPAAnnotationSurgeBufferEnabled = "vpa-rollout.influxdata.io/surge-buffer-enabled"

	// Override the number of surge buffer pods to create for the VPA's target workload during a rollout. Default is 1.
	VPAAnnotationNumberOfSurgeBufferPods = "vpa-rollout.influxdata.io/number-of-surge-buffer-pods"

	// Default number of surge buffer pods if not specified in the VPA annotation
	DefaultSurgeBufferReplicas = "1"

	// Annotation to indicate that the Pod is a "surge-buffer" pod
	PodAnnotationSurgeBufferPod = "vpa-rollout.influxdata.io/surge-buffer"
)

var (
	SurgeBufferPodAnnotations = map[string]interface{}{
		PodAnnotationSurgeBufferPod:                      "true",
		"cluster-autoscaler.kubernetes.io/safe-to-evict": "false", // Prevent cluster autoscaler from evicting this pod
		"sidecar.istio.io/inject":                        "false",
	}
)
