/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Note: the example only works with the code within the same release/branch.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/zach-robinson-dev/kollections/pkg/comparator"
	"github.com/zach-robinson-dev/kollections/pkg/list"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	watcher "k8s.io/client-go/tools/watch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/influxdata/vpa-rollout-controller/pkg/utils"
)

const (
	// Default values for command-line flags
	diffPercentTriggerDefault     = 10
	cooldownPeriodDurationDefault = 15 * time.Minute
	loopWaitTimeInSecondsDefault  = 10
	patchOperationFieldManager    = "flux-client-side-apply"
)

func main() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.SetLogger(zap.New(zap.UseDevMode(true)))
	ctx := log.IntoContext(context.Background(), ctrl.Log.WithName("vpa-rollout-controller"))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stop, watcherDone, handlerDone := beginWatch(ctx)

	<-sigCh
	stop()
	<-watcherDone
	<-handlerDone
}

func beginWatch(ctx context.Context) (func(), <-chan struct{}, <-chan struct{}) {
	done := make(chan struct{})

	logger := log.FromContext(ctx)

	// Command-line flags with default values
	diffPercentTriggerDefault := flag.Int("diffPercentTrigger", diffPercentTriggerDefault, "Percentage difference to trigger rollout")
	cooldownPeriodInMinutesDefault := flag.Duration("cooldownPeriod", cooldownPeriodDurationDefault, "Cooldown period before triggering another rollout")
	loopWaitTimeInSecondsDefault := flag.Int("loopWaitTime", loopWaitTimeInSecondsDefault, "Time to wait between each loop iteration")
	patchOperationFieldManagerDefault := flag.String("patchOperationFieldManager", patchOperationFieldManager, "Field manager for patch operations")
	flag.Parse()
	diffPercentTrigger := *diffPercentTriggerDefault
	cooldownPeriodDuration := *cooldownPeriodInMinutesDefault
	loopWaitTimeDuration := time.Duration(*loopWaitTimeInSecondsDefault) * time.Second
	patchOperationFieldManager := *patchOperationFieldManagerDefault
	logger.V(1).Info("Starting VPA Rollout Controller with parameters", "diffPercentTrigger", diffPercentTrigger, "cooldownPeriodDuration", cooldownPeriodDuration, "loopWaitTimeDuration", loopWaitTimeDuration, "patchOperationFieldManager", patchOperationFieldManager)

	// Setup client-go
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	vpaClient, err := vpaclientset.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// List Once on launch and process all VPAs

	vpaList, err := vpaClient.AutoscalingV1().VerticalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	logger.V(1).Info("Processing initial list of VPAs in the cluster", "Total", len(vpaList.Items))

	for _, vpa := range vpaList.Items {
		processVPAItem(ctx, &vpa, clientset, dynamicClient, diffPercentTrigger, cooldownPeriodDuration)
	}

	toResourceVersions := func(vpa v1.VerticalPodAutoscaler) int {
		if res, err := strconv.Atoi(vpa.ResourceVersion); err != nil {
			panic(err)
		} else {
			return res
		}
	}

	vpas := list.List[v1.VerticalPodAutoscaler](vpaList.Items)
	vpaRVs := list.Map(vpas, toResourceVersions)
	initialRv := vpaRVs.MaxWith(comparator.AscendingOrder[int]())

	watchFunc := func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
		timeOut := int64(60)
		return vpaClient.AutoscalingV1().VerticalPodAutoscalers(metav1.NamespaceAll).Watch(ctx, metav1.ListOptions{TimeoutSeconds: &timeOut})
	}

	retryWatcher, err := watcher.NewRetryWatcherWithContext(ctx, strconv.Itoa(initialRv), &cache.ListWatch{WatchFuncWithContext: watchFunc})
	if err != nil {
		panic(err)
	}

	// Watch for changes to VPAs
	go func() {
		defer close(done)

		for event := range retryWatcher.ResultChan() {
			switch event.Type {
			case watch.Deleted:
				// If a VPA is deleted then we don't need to do anything
			case watch.Bookmark:
				// The RetryWatcher handles these for us
			case watch.Error:
				if err, ok := event.Object.(error); ok {
					logger.Error(err, "error in VPA watch")
				} else {
					logger.Info("watch error event in VPA watch", "error", event.Object)
				}
			case watch.Modified:
				fallthrough
			case watch.Added:
				processVPAItem(ctx, event.Object.(*v1.VerticalPodAutoscaler), clientset, dynamicClient, diffPercentTrigger, cooldownPeriodDuration)
			}
		}
	}()

	return retryWatcher.Stop, retryWatcher.Done(), done
}

func processVPAItem(ctx context.Context, vpa *v1.VerticalPodAutoscaler, clientset *kubernetes.Clientset, dynamicClient dynamic.Interface, diffPercentTrigger int, cooldownPeriodDuration time.Duration) {
	logger := log.FromContext(ctx)

	// Check if the VPA is eligible for processing
	if !utils.VPAIsEligible(ctx, vpa) {
		return
	}
	logger.V(1).Info("Processing VPA", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)

	// Get the VPA's target workload resource
	workload, err := utils.GetTargetWorkload(ctx, vpa, dynamicClient)
	if err != nil {
		logger.Error(err, "Error fetching target workload:")
		return
	}
	workloadName := workload["metadata"].(map[string]interface{})["name"]
	workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

	// Check if a rollout is needed
	rolloutIsNeeded, err := utils.RolloutIsNeeded(ctx, clientset, vpa, workload, diffPercentTrigger)
	if err != nil {
		logger.Error(err, "Error checking if rollout is needed:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
		return
	}
	if rolloutIsNeeded {
		// Check if the cooldown period has elapsed
		cooldownHasElapsed, err := utils.CooldownHasElapsed(ctx, clientset, vpa, workload, cooldownPeriodDuration)
		if err != nil {
			logger.Error(err, "Error checking cooldown period:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
			return
		}
		if cooldownHasElapsed {
			err := utils.TriggerRollout(ctx, workload, dynamicClient, patchOperationFieldManager)
			if err != nil {
				logger.Error(err, "Error triggering rollout:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
				return
			}
		}
	}
}
