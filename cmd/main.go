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
	"log/slog"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_clientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	c "github.com/influxdata/vpa-rollout-controller/internal/controller"
)

const (
	// Default values for command-line flags
	diffTriggerPercentageDefault      = 10
	cooldownPeriodDurationDefault     = 15 * time.Minute
	loopWaitTimeSecondsDefault        = 30
	patchOperationFieldManagerDefault = "flux-client-side-apply"
)

func main() {

	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(log)

	// Command-line flags with default values
	diffTriggerPercentageDefault := flag.Int("diffTriggerPercentage", diffTriggerPercentageDefault, "Percentage difference to trigger rollout")
	cooldownPeriodDurationDefault := flag.Duration("cooldownPeriodDuration", cooldownPeriodDurationDefault, "Cooldown period before triggering another rollout")
	loopWaitTimeSecondsDefault := flag.Int("loopWaitTimeSeconds", loopWaitTimeSecondsDefault, "Time to wait between each loop iteration")
	patchOperationFieldManagerDefault := flag.String("patchOperationFieldManager", patchOperationFieldManagerDefault, "Field manager for patch operations")
	flag.Parse()
	diffTriggerPercentage := *diffTriggerPercentageDefault
	cooldownPeriodDuration := *cooldownPeriodDurationDefault
	loopWaitTimeDuration := time.Duration(*loopWaitTimeSecondsDefault) * time.Second
	patchOperationFieldManager := *patchOperationFieldManagerDefault
	log.Info("Starting VPA Rollout Controller with parameters", "diffTriggerPercentage", diffTriggerPercentage, "cooldownPeriodDuration", cooldownPeriodDuration, "loopWaitTimeDuration", loopWaitTimeDuration, "patchOperationFieldManager", patchOperationFieldManager)

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
		log.Info("Processing list of VPAs in the cluster", "Total", len(vpas.Items))

		for _, vpa := range vpas.Items {

			// Check if the VPA is eligible for processing
			if !c.VPAIsEligible(ctx, vpa) {
				continue
			}
			log.Info("Processing VPA", "Name", vpa.Name, "Namespace", vpa.Namespace, "WorkloadKind", vpa.Spec.TargetRef.Kind, "WorkloadName", vpa.Spec.TargetRef.Name)

			// Get the VPA's target workload resource
			workload, err := c.GetTargetWorkload(ctx, vpa, dynamicClient)
			if err != nil {
				log.Error("Error fetching target workload", "err", err)
				continue
			}
			workloadName := workload["metadata"].(map[string]interface{})["name"]
			workloadNamespace := workload["metadata"].(map[string]interface{})["namespace"]

			// Check if the cooldown period has elapsed
			cooldownHasElapsed, err := c.CooldownHasElapsed(ctx, clientset, vpa, workload, cooldownPeriodDuration)
			if err != nil {
				log.Error("Error checking cooldown period", "err", err, "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
				continue
			}
			if cooldownHasElapsed {
				// Check if a rollout is needed
				rolloutIsNeeded, err := c.RolloutIsNeeded(ctx, clientset, vpa, workload, diffTriggerPercentage)
				if err != nil {
					log.Error("Error checking if rollout is needed", "err", err, "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
					continue
				}
				if rolloutIsNeeded {
					err := c.TriggerRollout(ctx, workload, vpa, dynamicClient, patchOperationFieldManager)
					if err != nil {
						log.Error("Error triggering rollout", "err", err, "VPAName", vpa.Name, "WorkloadName", workloadName, "WorkloadNamespace", workloadNamespace)
						continue
					}
				}
			}
		}

		time.Sleep(loopWaitTimeDuration)
	}
}
