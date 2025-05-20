#!/bin/bash
# This script builds the Docker image and pushes it to Google Artifact Registry (GAR).
# It assumes that you have Go, Docker and Google Cloud SDK installed and configured on your system.
# Usage: ./scripts/docker-push-to-gar.sh [build_platform]
app_name="vpa-rollout-controller"
build_platform="${1:-amd64}"
tag="${2:-$(git rev-parse HEAD)}"
prefix="us-docker.pkg.dev/influxdb2-artifacts/tubernetes"
image="${prefix}/${app_name}:${tag}"

set -e

# Check if the user is authenticated with Google Cloud
if ! gcloud auth list --filter=status:ACTIVE --format="value(account)" | grep -q '@'; then
  echo "Please authenticate with Google Cloud using 'gcloud auth login'."
  exit 1
fi


just docker-build ${build_platform} ${tag}
docker image tag "${app_name}:${tag}" "${image}"
docker image push "${image}"