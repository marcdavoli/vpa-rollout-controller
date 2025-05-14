# Vertical Pod Autoscaler Rollout Controller

The vpa-rollout-controller is a simple Kubernetes controller based on client-go. It is meant to provide better availability for Kubernetes workloads that use the Vertical Pod Autoscaler and allows them to 'surge' (using `maxSurge`). The upstream VPA Updater component explicitly evicts individual pods to force them to update their 'requests', which prevents the ability to 'surge' during rollouts. This controller works alongside the existing VPA components and adds the possibility of rolling out pod 'requests' changes by triggering a rollout equivalent to that of the `kubectl rollout restart` command.

To avoid The vpa-rollout-controller will `VerticalPodAutoscalers` 

## Table of Contents

- [Vertical Pod Autoscaler Rollout Controller](#vertical-pod-autoscaler-rollout-controller)
  - [Table of Contents](#table-of-contents)
  - [Running Locally](#running-locally)
  - [Requirements](#requirements)
    - [`VerticalPodAutoscaler` Resources](#verticalpodautoscaler-resources)
    - [`ClusterRole` \& `ClusterRoleBinding` Permissions](#clusterrole--clusterrolebinding-permissions)
    - [Usage](#usage)
  - [TO DO](#to-do)
  - [CLI Flags](#cli-flags)
  - [Annotations](#annotations)

## Running Locally
This app is meant to run as a pod inside a Kubernetes cluster, for which it will 

- Install `kind` and `docker`.
- `make dev` to build a kind cluster with local registry
- `make run` to build and deploy the controller


## Requirements

### `VerticalPodAutoscaler` Resources
- VPA resources must opt-in to be managed by the vpa-rollout-controller, by including the annotation `vpa-rollout.influxdata.io/enabled: "true"`.
- To ensure VPAs are not evicted by the upstream VPA's `Updater` component, the VPA resources must have the field `spec.updatePolicy.updateMode` set to `Initial`. 
- The Kubernetes workload resource (Deployment, StatefulSet, DaemonSet, etc.) targeted by the VPA must support the `kubectl.kubernetes.io/restartedAt` annotation for the controller to function.

### `ClusterRole` & `ClusterRoleBinding` Permissions
- Should be able to List and Read VPA resources
- Patch the Target workload resources (Deployments, StatefulSets, DaemonSets, etc.)


### Usage

TBD

## TO DO
- Getting the workload requests should be done on running pods, not workload resource itself, because the mutating webhook doesn't update the workload manifest

## CLI Flags

The following table lists the CLI flags supported by the vpa-rollout-controller:

| Flag | Type | Default Value | Description |
|------|------|---------------|-------------|
| `diffPercentTrigger` | int | 10 | Percentage difference between VPA recommendation and current resources that triggers a rollout |
| `cooldownPeriod` | duration | 15m | Cooldown period before triggering another rollout for the same workload |
| `loopWaitTime` | int | 10 | Time in seconds to wait between each loop iteration |
| `patchOperationFieldManager` | string | "flux-client-side-apply" | Field manager name for patch operations |

## Annotations

These annotations can be added to `VerticalPodAutoscaler` resources to customize the behavior of the vpa-rollout-controller:

| Annotation | Type | Default | Description |
|------------|------|---------|-------------|
| `vpa-rollout.influxdata.io/enabled` | boolean | - | Required annotation to enable a VPA to be managed by the controller. Must be set to `"true"` |
| `vpa-rollout.influxdata.io/cooldown-period` | duration | - | Override the default cooldown period for a specific VPA. Accepts a valid Go duration string (e.g., `"15m"`, `"1h"`) |
| `vpa-rollout.influxdata.io/diff-percent-trigger` | int | - | Override the default percentage difference that triggers a rollout for a specific VPA |