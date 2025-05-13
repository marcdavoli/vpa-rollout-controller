.PHONY: dev
dev:
	./scripts/create-kind-cluster-with-registry.sh


.PHONY: run
run:
	./scripts/build-and-run-controller-locally.sh

.PHONY: deploy
deploy:
	./scripts/deploy-controller.sh