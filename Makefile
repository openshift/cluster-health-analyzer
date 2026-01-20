# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Tool binaries
GOLANGCI_LINT := $(GOBIN)/golangci-lint
YQ := $(GOBIN)/yq
PROMTOOL := $(GOBIN)/promtool

# Include integration testing targets
include test.mk

# ----------------
# Help
# ----------------

## help> print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s '>' |  sed -e 's/^/ /'

# ----------------
# Lint
# ----------------

## lint> lint using golangci-lint
.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run --timeout 5m

$(GOLANGCI_LINT):
	./hack/install-golangci-lint.sh


# ----------------
# Test
# ----------------

## test> run unit tests (excludes integration tests)
.PHONY: test
test: 
	go test -race $$(go list ./... | grep -v /test/integration)

## test-verbose> run unit tests with verbose output (excludes integration tests)
.PHONY: test-verbose
test-verbose:
	go test -race -v $$(go list ./... | grep -v /test/integration)

# ----------------
# Develop
# ----------------

## proxy> port-forward to thanos-querier (requires oc login first)
.PHONY: proxy
proxy:
	./hack/listen-thanos.sh

## run> run the server locally (requires prometheus and alertmanager runnning)
.PHONY: run
run:
	go run ./main.go serve --disable-auth-for-testing

## run-mcp> run the mcp server locally (requires prometheus and alertmanager running)
.PHONY: run-mcp
run-mcp:
	go run ./main.go mcp

## generate> run go generate
.PHONY: generate
generate:
	go generate ./...


## simulate> simulate test data and creates cluster-health-analyzer-openmetrics.txt in openmetrics format
.PHONY: simulate
simulate:
	go run ./main.go simulate $(if $(SCENARIO),--scenario $(SCENARIO))

.PHONY: build
build:
	go build -o bin/cluster-health-analyzer . 

# ----------------
# Deploy
# ----------------

## deploy> deploy to a cluster (requires oc login first)
.PHONY: deploy
deploy: 
	oc apply -f manifests/backend manifests/frontend

## undeploy> remove the services from the cluster (requires oc login first)
.PHONY: undeploy
undeploy:
	oc delete -f manifests/backend manifests/frontend

## precommit> run linting and unit tests
.PHONY: precommit
precommit: lint test

# ----------------
# Integration Tests
# ----------------

GINKGO_COLOR := $(if $(CI),--no-color,)
GINKGO := go run github.com/onsi/ginkgo/v2/ginkgo $(GINKGO_COLOR)

$(YQ):
	./hack/install-yq.sh

$(PROMTOOL):
	./hack/install-promtool.sh
	
## install-tools> install all development tools (golangci-lint, yq, promtool)
.PHONY: install-integration-test-tools
install-integration-test-tools: $(GOLANGCI_LINT) $(YQ) $(PROMTOOL)

# Default values for integration tests
export CHA_IMAGE ?= quay.io/openshiftanalytics/cluster-health-analyzer:latest
export MANIFESTS_PATH ?= manifests/backend
export DEPLOYMENT_NAME ?= cluster-health-analyzer
export NAMESPACE ?= openshift-cluster-health-analyzer

## deploy-integration> deploy to cluster for integration testing
.PHONY: deploy-integration
deploy-integration:
	./hack/deploy-integration.sh

## undeploy-integration> remove integration test deployment
.PHONY: undeploy-integration
undeploy-integration:
	oc delete -f $(MANIFESTS_PATH)/ --ignore-not-found

## test-integration> run integration tests (assumes deployment exists)
.PHONY: test-integration
test-integration:
	$(GINKGO) -v ./test/integration/...
