APP_NAME := "vpa-rollout-controller"
DEFAULT_ARCH := "amd64"
LATEST := `git rev-parse --short HEAD`

# Runs 'just -l'
help:
    just -l

# go fmt, go vet and go test
test:
	go fmt ./...
	go vet ./...
	go test ./...

# Deploys a kind cluster with a local registry
dev:
    ./scripts/create-kind-cluster.sh

# Runs the controller in a local kind cluster
run arch=DEFAULT_ARCH:
    ./scripts/build-and-run-controller-locally.sh {{arch}}

# Builds the docker image for the selected platform.
docker-build arch=DEFAULT_ARCH tag=LATEST:
    docker build \
    --platform linux/{{arch}} \
    --build-arg=TARGETARCH={{arch}} \
    --build-arg=OPERATOR_VERSION={{tag}} \
    -t {{APP_NAME}}:{{tag}} .

# Pushes the docker image to Google Artifact Registry
push-to-gar arch=DEFAULT_ARCH TAG=LATEST:
    ./scripts/push-to-gar.sh {{arch}} {{TAG}}