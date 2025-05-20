package utils

import (
	"context"
	"testing"
	"time"

	testutil "github.com/influxdata/vpa-rollout-controller/test"
)

func TestCooldownHasElapsed(t *testing.T) {
	// Create a fake clientset using the utility function
	clientset := testutil.CreateTestClientset()

	// Create a fake VPA object using the utility function
	vpa := testutil.CreateTestVPA(
		testutil.WithName("test-vpa"),
		testutil.WithNamespace("default"),
		testutil.WithCooldownPeriod("5m"),
	)

	currentTime := time.Now().UTC().Format(time.RFC3339)
	workload := testutil.CreateTestWorkload("test-workload", "default", currentTime)

	// Set the default cooldown period duration, this should be overridden by the VPA annotation
	defaultCooldownPeriodDuration := 10 * time.Minute

	t.Run("Cooldown has not elapsed", func(t *testing.T) {
		cooldownHasElapsed, err := CooldownHasElapsed(context.Background(), clientset, vpa, workload, defaultCooldownPeriodDuration)
		if err != nil {
			t.Fatalf("Error checking cooldown: %v", err)
		}
		if cooldownHasElapsed {
			t.Errorf("Expected cooldown to not have elapsed, but it has")
		}
	})
}
