APP := 'vpa-rollout-controller'
DEFAULT_ARCH := "amd64"
LATEST := `git rev-parse --short HEAD`

test:
	go fmt ./...
	go vet ./...
## More coming soon

# Deploys a kind cluster with a local registry
dev:
    ./scripts/create-kind-cluster-with-registry.sh

# Runs the controller in a local kind cluster
run arch=DEFAULT_ARCH:
    ./scripts/build-and-run-controller-locally.sh {{arch}}

# Builds the docker image for the selected platform.
docker-build arch=DEFAULT_ARCH TAG="latest-{{arch}}":
    docker build \
    --platform linux/{{arch}} \
    --build-arg=TARGETARCH={{arch}} \
    --build-arg=OPERATOR_VERSION={{LATEST}} \
    -t {{APP}}:{{TAG}} .