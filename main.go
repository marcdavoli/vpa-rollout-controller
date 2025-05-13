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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_clientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	diffPercentTriggerDefault     = 10
	cooldownPeriodDurationDefault = 10 * time.Minute
	loopWaitTimeInSecondsDefault  = 10
)

func main() {

	ctx := context.Background()

	log.SetLogger(zap.New(zap.UseDevMode(true)))
	log := log.FromContext(ctx)

	// Command-line flags with default values
	diffPercentTriggerValue := flag.Int("diffPercentTrigger", diffPercentTriggerDefault, "Percentage difference to trigger rollout")
	cooldownPeriodInMinutes := flag.Duration("cooldownPeriod", cooldownPeriodDurationDefault, "Cooldown period before triggering another rollout")
	loopWaitTimeInSeconds := flag.Int("loopWaitTime", loopWaitTimeInSecondsDefault, "Time to wait between each loop iteration")
	flag.Parse()
	diffPercentTrigger := *diffPercentTriggerValue
	cooldownPeriodDuration := *cooldownPeriodInMinutes
	loopWaitTimeDuration := time.Duration(*loopWaitTimeInSeconds) * time.Second
	log.V(1).Info("Starting VPA Rollout Controller with parameters", "diffPercentTrigger", diffPercentTrigger, "cooldownPeriodDuration", cooldownPeriodDuration, "loopWaitTimeDuration", loopWaitTimeDuration)

	// Setup client-go
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Main loop
	for {
		vpaClient, err := vpa_clientset.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}
		vpas, err := vpaClient.AutoscalingV1().VerticalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		log.V(1).Info("Processing list of VPAs in the cluster", "Total", len(vpas.Items))

		for _, vpa := range vpas.Items {

			// Check if the VPA is eligible for processing
			if !vpaIsEligible(ctx, vpa) {
				continue
			}
			log.V(1).Info("Processing VPA", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)

			// Get the VPA's target workload resource
			workload, err := getTargetWorkload(ctx, vpa, dynamicClient)
			if err != nil {
				log.Error(err, "Error fetching target workload:")
				continue
			}
			workloadName := workload["metadata"].(map[string]interface{})["name"]
			workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

			// Check if a rollout is needed
			rolloutIsNeeded, err := rolloutIsNeeded(ctx, vpa, workload, diffPercentTrigger)
			if err != nil {
				log.Error(err, "Error checking if rollout is needed:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
				continue
			}
			if rolloutIsNeeded {
				// Check if the cooldown period has elapsed
				cooldownHasElapsed, err := cooldownHasElapsed(ctx, workload, cooldownPeriodDuration)
				if err != nil {
					log.Error(err, "Error checking cooldown period:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
					continue
				}
				if cooldownHasElapsed {
					err := triggerRollout(ctx, workload, dynamicClient)
					if err != nil {
						log.Error(err, "Error triggering rollout:", "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
						continue
					}
				}
			} else {
				log.V(1).Info("No rollout needed for VPA's Target Workload", "VPAName", vpa.Name, "VPANamespace", vpa.Namespace, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
			}
		}

		time.Sleep(loopWaitTimeDuration)
	}
}
