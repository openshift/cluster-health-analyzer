# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Tool binaries
GOLANGCI_LINT := $(GOBIN)/golangci-lint
YQ := $(or $(shell which yq 2>/dev/null),$(GOBIN)/yq)
PROMTOOL := $(or $(shell which promtool 2>/dev/null),$(GOBIN)/promtool)
MCPCHECKER_VERSION ?= 0.0.22
MCPCHECKER_OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
MCPCHECKER_ARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
MCPCHECKER := bin/mcpchecker

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
	go test -race -tags testonly ./cmd/serve ./cmd/mcp

## test-verbose> run unit tests with verbose output (excludes integration tests)
.PHONY: test-verbose
test-verbose:
	go test -race -v $$(go list ./... | grep -v /test/integration)

# ----------------
# Develop
# ----------------

## proxy> port-forward to thanos-querier and alertmanager (requires oc login first)
.PHONY: proxy
proxy:
	./hack/listen-thanos.sh & \
	./hack/listen-alertmanager.sh & \
	wait

## run> run a test-only local server (requires prometheus and alertmanager runnning)
.PHONY: run
run:
	go run -tags testonly ./main.go serve --disable-auth-for-testing

## run-mcp> run the mcp server locally (requires prometheus and alertmanager running)
.PHONY: run-mcp
run-mcp:
	go run -tags testonly ./main.go mcp --disable-auth-for-testing

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
# Evals (mcpchecker)
# ----------------

$(MCPCHECKER):
	@echo "==> Installing mcpchecker v$(MCPCHECKER_VERSION) ($(MCPCHECKER_OS)/$(MCPCHECKER_ARCH))..."
	@mkdir -p bin
	@curl -fsSL -o bin/mcpchecker.zip \
		https://github.com/mcpchecker/mcpchecker/releases/download/v$(MCPCHECKER_VERSION)/mcpchecker-$(MCPCHECKER_OS)-$(MCPCHECKER_ARCH).zip
	@unzip -o -q bin/mcpchecker.zip -d bin
	@rm -f bin/mcpchecker.zip
	@chmod +x bin/mcpchecker
	@echo "==> mcpchecker v$(MCPCHECKER_VERSION) installed to $(MCPCHECKER)"

## install-mcpchecker> install mcpchecker CLI for running evals
.PHONY: install-mcpchecker
install-mcpchecker: $(MCPCHECKER)

MCPCHECKER_EVAL_DIR := evals/mcpchecker
RUNS ?= 1
EVAL_CONFIG ?= eval.yaml

export TASK
export CATEGORY
export RUNS
export EVAL_CONFIG

## run-evals> run mcpchecker evals (TASK=name, CATEGORY=..., RUNS=1); starts backends and the MCP server automatically (requires oc login and OPENAI_API_KEY)
.PHONY: run-evals
run-evals: $(MCPCHECKER)
	./hack/run-mcpchecker-eval.sh
