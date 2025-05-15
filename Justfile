test:
	go fmt ./...
	go vet ./...
## More coming soon

# Deploys a kind cluster with a local registry
dev:
    ./scripts/create-kind-cluster-with-registry.sh

# Runs the controller in a local kind cluster
run:
    ./scripts/build-and-run-controller-locally.sh

################# Docker recipes #############
IMG := "controller:latest"
BUILD_PLATFORMS := "linux/arm64,linux/amd64"
LATEST := `git rev-parse --short HEAD`
# Builds the docker image for the selected platform. Usage: just docker-build amd64
docker-build arch='amd64':
    docker build \
    --platform linux/{{arch}} \
    --build-arg=TARGETARCH={{arch}} \
    --build-arg=OPERATOR_VERSION={{LATEST}} \
    -t {{IMG}}-{{arch}} .