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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa_clientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	diffPercentTrigger = 10
	cooldownPeriod     = 10 * time.Minute
	loopWaitTime       = 10 * time.Second
)

func main() {
	ctx := context.Background()

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	for {
		vpaClient, err := vpa_clientset.NewForConfig(config)
		if err != nil {
			panic(err.Error())
		}
		vpas, err := vpaClient.AutoscalingV1().VerticalPodAutoscalers(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}
		fmt.Printf("There are %d vpas in the cluster\n", len(vpas.Items))

		for _, vpa := range vpas.Items {
			fmt.Printf("Processing VPA Name: %s, Namespace: %s, Target: %s/%s\n", vpa.Name, vpa.Namespace, vpa.Spec.TargetRef.Kind, vpa.Spec.TargetRef.Name)

			workload, err := getTargetWorkload(ctx, vpa, dynamicClient)
			if err != nil {
				fmt.Printf("Error fetching target workload: %v\n", err)
				continue
			}

			if rolloutIsNeeded(ctx, vpa, workload) {
				fmt.Printf("Rollout is needed for VPA %s\n", vpa.Name)
				if cooldownHasElapsed(ctx, workload) {
					triggerRollout(ctx, workload, dynamicClient)
				} else {
					fmt.Printf("Cooldown period has not elapsed for VPA %s\n", vpa.Name)
				}
			} else {
				fmt.Printf("No rollout needed for VPA's Target Workload %s\n", vpa.Name)
			}
		}

		time.Sleep(loopWaitTime)
	}
}
