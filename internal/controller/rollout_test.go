package controller

import (
	"context"
	"testing"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes/fake"

	testutil "github.com/influxdata/vpa-rollout-controller/test"
)

func TestRolloutIsNeeded(t *testing.T) {
	ctx := context.Background()
	clientset := fake.NewSimpleClientset()
	vpa := testutil.CreateTestVPA(
		testutil.WithStatus(
			testutil.WithRecommendation(
				testutil.WithTargetCPU(resource.MustParse("200m")),
				testutil.WithTargetMemory(resource.MustParse("200Mi")),
			),
		),
		testutil.WithAnnotation(utils.VPAAnnotationDiffPercentTrigger, "10"),
	)
	workload := testutil.CreateTestWorkload("my-workload", "default", "2025-01-01T00:00:00Z")
	diffPercentTrigger := 10

	rolloutIsNeeded, err := RolloutIsNeeded(ctx, clientset, vpa, workload, diffPercentTrigger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rolloutIsNeeded {
		t.Fatalf("expected no rollout needed, but it is")
	}

}

func TestTriggerRollout(t *testing.T) {
	ctx := context.Background()
	// Use the test FakeDynamicClient to avoid real API calls
	dynamicClient := &testutil.FakeDynamicClient{}
	workload := testutil.CreateTestWorkload("my-workload", "default", "2025-01-01T00:00:00Z")
	patchOperationFieldManager := "test-field-manager"

	err := TriggerRollout(ctx, workload, dynamicClient, patchOperationFieldManager)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}
