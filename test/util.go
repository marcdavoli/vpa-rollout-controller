package test

import (
	"context"
	"fmt"

	autoscaling "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"
)

type FakeDynamicClient struct {
	ShouldError bool
}

func (f *FakeDynamicClient) Resource(resource schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return &FakeNamespaceableResource{ShouldError: f.ShouldError}
}

type FakeNamespaceableResource struct {
	ShouldError bool
}

func (f *FakeNamespaceableResource) Namespace(ns string) dynamic.ResourceInterface {
	return &FakeResource{ShouldError: f.ShouldError}
}
func (f *FakeNamespaceableResource) Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) Apply(ctx context.Context, name string, declarativeObj *unstructured.Unstructured, opts metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) ApplyStatus(ctx context.Context, name string, declarativeObj *unstructured.Unstructured, opts metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) Delete(ctx context.Context, name string, opts metav1.DeleteOptions, subresources ...string) error {
	return nil
}
func (f *FakeNamespaceableResource) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeNamespaceableResource) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}
func (f *FakeNamespaceableResource) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

type FakeResource struct {
	ShouldError bool
}

func (f *FakeResource) Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if f.ShouldError {
		return nil, fmt.Errorf("simulated error")
	}
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]interface{}{
		"kind": "Deployment",
		"metadata": map[string]interface{}{
			"name":      "test-deployment",
			"namespace": "default",
		},
	}
	return obj, nil
}
func (f *FakeResource) Apply(ctx context.Context, name string, declarativeObj *unstructured.Unstructured, opts metav1.ApplyOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeResource) ApplyStatus(ctx context.Context, name string, declarativeObj *unstructured.Unstructured, opts metav1.ApplyOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeResource) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}
func (f *FakeResource) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return nil, nil
}
func (f *FakeResource) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeResource) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeResource) Delete(ctx context.Context, name string, opts metav1.DeleteOptions, subresources ...string) error {
	return nil
}
func (f *FakeResource) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (f *FakeResource) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}
func (f *FakeResource) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	return nil, nil
}

// CreateTestVPA creates a VPA object for testing with configurable parameters
func CreateTestVPA(options ...VPAOption) vpa_types.VerticalPodAutoscaler {
	// Default values
	mode := vpa_types.UpdateModeInitial
	vpa := vpa_types.VerticalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vpa",
			Namespace: "default",
			Annotations: map[string]string{
				utils.VPAAnnotationCooldownPeriod: "5m",
			},
		},
		Spec: vpa_types.VerticalPodAutoscalerSpec{
			TargetRef: &autoscaling.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       "test-deployment",
				APIVersion: "apps/v1",
			},
			UpdatePolicy: &vpa_types.PodUpdatePolicy{
				UpdateMode: &mode,
			},
		},
	}

	// Apply all options
	for _, option := range options {
		option(&vpa)
	}

	return vpa
}

// VPAOption defines a function type to apply options to a VPA
type VPAOption func(*vpa_types.VerticalPodAutoscaler)

// WithName sets the name of the VPA
func WithName(name string) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		vpa.Name = name
	}
}

// WithNamespace sets the namespace of the VPA
func WithNamespace(namespace string) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		vpa.Namespace = namespace
	}
}

// WithTargetRef sets the target reference for the VPA
func WithTargetRef(kind, name, apiVersion string) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		vpa.Spec.TargetRef = &autoscaling.CrossVersionObjectReference{
			Kind:       kind,
			Name:       name,
			APIVersion: apiVersion,
		}
	}
}

// WithUpdateMode sets the update mode for the VPA
func WithUpdateMode(mode vpa_types.UpdateMode) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		if vpa.Spec.UpdatePolicy == nil {
			vpa.Spec.UpdatePolicy = &vpa_types.PodUpdatePolicy{}
		}
		vpa.Spec.UpdatePolicy.UpdateMode = &mode
	}
}

// WithCooldownPeriod sets the cooldown period annotation
func WithCooldownPeriod(duration string) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		if vpa.Annotations == nil {
			vpa.Annotations = make(map[string]string)
		}
		vpa.Annotations[utils.VPAAnnotationCooldownPeriod] = duration
	}
}

// WithAnnotation sets a custom annotation
func WithAnnotation(key, value string) VPAOption {
	return func(vpa *vpa_types.VerticalPodAutoscaler) {
		if vpa.Annotations == nil {
			vpa.Annotations = make(map[string]string)
		}
		vpa.Annotations[key] = value
	}
}

// CreateTestWorkload creates a test workload object for testing
func CreateTestWorkload(name, namespace string, restartedAt string) map[string]interface{} {
	workload := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]interface{}{
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"app": "myapp"},
			},
		},
	}

	if restartedAt != "" {
		// Add the restartedAt annotation
		annotations := map[string]interface{}{
			"kubectl.kubernetes.io/restartedAt": restartedAt,
		}
		workloadMeta := workload["metadata"].(map[string]interface{})
		workloadMeta["annotations"] = annotations
	}

	return workload
}

// CreateTestClientset creates a fake Kubernetes clientset for testing
func CreateTestClientset() *fake.Clientset {
	return fake.NewSimpleClientset()
}
