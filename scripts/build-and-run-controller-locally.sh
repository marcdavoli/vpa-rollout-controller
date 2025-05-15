#!/usr/bin/env bash
# This script build the Docker image and app, and runs it in a Kubernetes cluster using Kind.
# It assumes that you have Go, Docker and Kind installed and configured on your system.
# Usage: ./scripts/build-and-run-controller.sh

set -e

# Housekeeping
echo "Housekeeping..."
kubectl delete pod vpa-rollout-controller || true

# Build the Go application and build and push a Docker image to local registry
echo "Building the Go application and Docker image..."
docker build -t in-cluster .
random_tag=$(openssl rand -hex 4)
docker tag in-cluster localhost:5001/in-cluster:${random_tag}
docker push localhost:5001/in-cluster:${random_tag}
kind load docker-image localhost:5001/in-cluster:${random_tag}

# Run the Docker image in a Kubernetes pod
echo "Running the Docker image in a Kubernetes pod..."
kubectl run vpa-rollout-controller --image=localhost:5001/in-cluster:${random_tag}
sleep 5
echo "Tailing pod logs... Press Ctrl+C to exit."
kubectl logs -f vpa-rollout-controller