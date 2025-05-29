package controller

import (
	"context"
	"testing"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/kubernetes/fake"

	testutil "github.com/influxdata/vpa-rollout-controller/test"
)

func TestVPAIsEligible(t *testing.T) {
	ctx := context.Background()

	// Create a test VPA with the enabled annotation
	vpa := testutil.CreateTestVPA(
		testutil.WithAnnotation(utils.VPAAnnotationEnabled, "true"),
	)

	if !VPAIsEligible(ctx, vpa) {
		t.Errorf("VPAIsEligible should return true when annotation is set and updateMode is Initial")
	}

	// Negative case: annotation missing
	vpaNoAnnotation := testutil.CreateTestVPA()
	// Remove the annotation
	vpaNoAnnotation.Annotations = map[string]string{}

	if VPAIsEligible(ctx, vpaNoAnnotation) {
		t.Errorf("VPAIsEligible should return false when annotation is missing")
	}

	// Negative case: annotation present but updateMode is not Initial
	otherMode := vpa_types.UpdateModeAuto
	vpaWrongMode := testutil.CreateTestVPA(
		testutil.WithAnnotation(utils.VPAAnnotationEnabled, "true"),
		testutil.WithUpdateMode(otherMode),
	)

	if VPAIsEligible(ctx, vpaWrongMode) {
		t.Errorf("VPAIsEligible should return false when updateMode is not Initial")
	}
}

func TestGetTargetWorkload(t *testing.T) {
	ctx := context.Background()
	fakeDynamic := &testutil.FakeDynamicClient{}

	// Create test VPA using the utility function
	vpa := testutil.CreateTestVPA()

	// Success case
	workload, err := GetTargetWorkload(ctx, vpa, fakeDynamic)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if workload["kind"] != "Deployment" {
		t.Errorf("expected kind Deployment, got: %v", workload["kind"])
	}

	// Error case
	fakeDynamic.ShouldError = true
	_, err = GetTargetWorkload(ctx, vpa, fakeDynamic)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

func TestGetTargetWorkloadPods(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels:    map[string]string{"app": "myapp"},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		},
	)
	workload := testutil.CreateTestWorkload("mydeployment", "default", "")
	pods, err := getTargetWorkloadPods(ctx, workload, client)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(pods.Items) != 1 {
		t.Errorf("expected 1 pod, got: %d", len(pods.Items))
	}
	if pods.Items[0].Status.Phase != corev1.PodRunning {
		t.Errorf("expected pod status to be Running, got: %v", pods.Items[0].Status.Phase)
	}
}

func TestWorkloadPodsAreHealthy(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels:    map[string]string{"app": "myapp"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "c1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{},
					},
				}},
			},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Name: "c1", Ready: true}},
			},
		},
	)
	workload := testutil.CreateTestWorkload("mydeployment", "default", "")
	healthy, err := workloadPodsAreHealthy(ctx, workload, client)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !healthy {
		t.Errorf("expected pods to be healthy")
	}

	// unhealthy: pod not running
	client = fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
				Labels:    map[string]string{"app": "myapp"},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
	)
	healthy, err = workloadPodsAreHealthy(ctx, workload, client)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if healthy {
		t.Errorf("expected pods to be unhealthy when not running")
	}
}

func TestGetTargetWorkloadPods_SurgeBufferLabelSelection(t *testing.T) {
	ctx := context.Background()
	selectorLabels := map[string]interface{}{"app": "myapp"}

	// Pod with selectorLabels only
	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	// Pod with selectorLabels and surge buffer label
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod2",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp", utils.PodLabelSurgeBufferPod: "true"},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	client := fake.NewSimpleClientset(pod1, pod2)

	// Workload that is NOT a surge-buffer (should exclude surge buffer pods)
	workload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "mydeployment",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": selectorLabels,
			},
		},
	}
	pods, err := getTargetWorkloadPods(ctx, workload, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pods.Items) != 1 || pods.Items[0].Name != "pod1" {
		t.Errorf("expected only pod1 (non-surge-buffer), got: %v", podNames(pods.Items))
	}

	// Workload that IS a surge-buffer (should select only surge buffer pods)
	workloadSurge := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "mydeployment-surge-buffer",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": selectorLabels,
			},
		},
	}
	podsSurge, err := getTargetWorkloadPods(ctx, workloadSurge, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(podsSurge.Items) != 1 || podsSurge.Items[0].Name != "pod2" {
		t.Errorf("expected only pod2 (surge-buffer), got: %v", podNames(podsSurge.Items))
	}
}

func podNames(pods []corev1.Pod) []string {
	names := make([]string, len(pods))
	for i, p := range pods {
		names[i] = p.Name
	}
	return names
}
