# Vertical Pod Autoscaler Rollout Controller

The vpa-rollout-controller is a simple Kubernetes controller based on client-go. It is meant to provide better availability for Kubernetes workloads that use the Vertical Pod Autoscaler and allows them to 'surge' (using `maxSurge`). The upstream VPA Updater component explicitly evicts individual pods to force them to update their 'requests', which prevents the ability to 'surge' during rollouts. This controller works alongside the existing VPA components and adds the possibility of rolling out pod 'requests' changes by triggering a rollout equivalent to that of the `kubectl rollout restart` command.

To avoid The vpa-rollout-controller will `VerticalPodAutoscalers` 

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

CLI flags

```yaml
args:
- '--cooldownPeriodMinutes 10'
- '--diffPercentTrigger 10'
- '--loopWaitTimeSeconds 10'
```