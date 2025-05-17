#!/usr/bin/env bash
# This script build the Docker image and app, and runs it in a Kubernetes cluster using Kind.
# It assumes that you have Go, Docker and Kind installed and configured on your system.
# Usage: ./scripts/build-and-run-controller.sh [build_platform]

app_name="vpa-rollout-controller"
registry_host="localhost:5001"
latest_sha=$(git rev-parse --short HEAD)
build_platform="${1:-amd64}"
set -e

# Build and push a new Docker image to local registry
echo "Building the Go application and Docker image..."
just docker-build ${build_platform} ${latest_sha}
docker image tag ${app_name}:${latest_sha} ${registry_host}/${app_name}:${latest_sha}
docker image push ${registry_host}/${app_name}:${latest_sha}

# Load the Docker image into the Kind cluster's registry
kind load docker-image ${registry_host}/${app_name}:${latest_sha}

# Run the Docker image in a Kubernetes pod
echo "Running the Docker image in a Kubernetes pod..."
kubectl wait serviceaccount/default --for=create
kubectl delete pod vpa-rollout-controller --ignore-not-found
kubectl run vpa-rollout-controller --image=${registry_host}/${app_name}:${latest_sha}
kubectl wait pod/vpa-rollout-controller --for condition=Ready
echo "Tailing pod logs... Press Ctrl+C to exit."
kubectl logs -f vpa-rollout-controller