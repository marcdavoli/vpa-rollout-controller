package utils

const (
	// Enables the vpa-rollout controller to operate on the VPA
	VPAAnnotationEnabled = "vpa-rollout.influxdata.io/enabled"

	// Override the cooldown period between rollouts for a specific VPA
	VPAAnnotationCooldownPeriod = "vpa-rollout.influxdata.io/cooldown-period"

	// Override the percentage difference that will trigger a rollout for the VPA's target workload
	VPAAnnotationDiffPercentTrigger = "vpa-rollout.influxdata.io/diff-percent-trigger"
)
