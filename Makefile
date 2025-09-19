# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif
GOLANGCI_LINT := $(GOBIN)/golangci-lint

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

## test> run unit tests
.PHONY: test
test: 
	go test -race ./...

## test-verbose> run unit tests with verbose output
.PHONY: test-verbose
test-verbose:
	go test -race -v ./...

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
