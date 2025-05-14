# Deploys a kind cluster with a local registry
.PHONY: dev
dev:
	./scripts/create-kind-cluster-with-registry.sh


# Runs the controller locally in a kind cluster
.PHONY: run
run:
	./scripts/build-and-run-controller-locally.sh

# Deploy the controller to the current kubectl context
#.PHONY: deploy
#deploy:
#	./scripts/deploy-controller-to-current-context.sh