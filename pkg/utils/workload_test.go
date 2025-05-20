package utils

import (
	"context"
	"testing"

	autoscaling "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"

	testutil "github.com/influxdata/vpa-rollout-controller/test"
)

func TestVPAIsEligible(t *testing.T) {
	ctx := context.Background()
	mode := vpa_types.UpdateModeInitial

	vpa := vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vpa",
			Namespace: "default",
			Annotations: map[string]string{
				VPAAnnotationEnabled: "true",
			},
		},
		Spec: vpa_types.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
			},
			UpdatePolicy: &vpa_types.PodUpdatePolicy{
				UpdateMode: &mode,
			},
		},
	}

	if !VPAIsEligible(ctx, vpa) {
		t.Errorf("VPAIsEligible should return true when annotation is set and updateMode is Initial")
	}

	// Negative case: annotation missing
	vpaNoAnnotation := vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vpa",
			Namespace: "default",
		},
		Spec: vpa_types.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
			},
			UpdatePolicy: &vpa_types.PodUpdatePolicy{
				UpdateMode: &mode,
			},
		},
	}
	if VPAIsEligible(ctx, vpaNoAnnotation) {
		t.Errorf("VPAIsEligible should return false when annotation is missing")
	}

	// Negative case: annotation present but updateMode is not Initial
	otherMode := vpa_types.UpdateModeAuto
	vpaWrongMode := vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vpa",
			Namespace: "default",
			Annotations: map[string]string{
				VPAAnnotationEnabled: "true",
			},
		},
		Spec: vpa_types.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
			},
			UpdatePolicy: &vpa_types.PodUpdatePolicy{
				UpdateMode: &otherMode,
			},
		},
	}
	if VPAIsEligible(ctx, vpaWrongMode) {
		t.Errorf("VPAIsEligible should return false when updateMode is not Initial")
	}
}

func TestGetTargetWorkload(t *testing.T) {
	ctx := context.Background()
	fakeDynamic := &testutil.FakeDynamicClient{}
	vpa := vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vpa",
			Namespace: "default",
		},
		Spec: vpa_types.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "test-deployment",
			},
		},
	}

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
	workload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "mydeployment",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": "myapp"},
			},
		},
	}
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
	workload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "mydeployment",
			"namespace": "default",
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": "myapp"},
			},
		},
	}
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
