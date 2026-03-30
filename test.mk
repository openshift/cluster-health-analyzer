# ----------------
# Integration Tests
# ----------------
# Targets for running integration tests against a live cluster
# Included by main Makefile
#
# Typical workflow:
#   1. make proxy              - port-forward to thanos-querier
#   2. make deploy-integration - deploy to cluster
#   3. make test-integration   - run tests
#   4. make undeploy-integration - cleanup

# Tool binaries for integration testing
YQ := $(or $(shell which yq 2>/dev/null),$(GOBIN)/yq)
PROMTOOL := $(or $(shell which promtool 2>/dev/null),$(GOBIN)/promtool)

GINKGO_COLOR := $(if $(CI),--no-color,)
GINKGO := go run github.com/onsi/ginkgo/v2/ginkgo $(GINKGO_COLOR)
export GOFLAGS=-mod=readonly

$(YQ):
	./hack/install-yq.sh

$(PROMTOOL):
	./hack/install-promtool.sh

## install-integration-test-tools> install tools needed for integration tests (yq, promtool)
.PHONY: install-integration-test-tools
install-integration-test-tools: $(YQ) $(PROMTOOL)

# Default values for integration tests
export CHA_IMAGE ?= quay.io/openshiftanalytics/cluster-health-analyzer:latest
export MANIFESTS_PATH ?= manifests/backend
export DEPLOYMENT_NAME ?= cluster-health-analyzer
export NAMESPACE ?= openshift-cluster-health-analyzer

## env-coo> output env vars for testing against COO production deployment (use: eval $$(make env-coo))
.PHONY: env-coo
env-coo:
	@echo 'export DEPLOYMENT_NAME=health-analyzer'
	@echo 'export NAMESPACE=openshift-cluster-observability-operator'

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
	$(GINKGO) -v --label-filter="!stress&&!stress-simulate" ./test/integration/...

# Stress test configuration
export STRESS_ALERT_COUNT ?= 500
export STRESS_ALERT_TIMEOUT_MIN ?= 6
export STRESS_INCIDENT_TIMEOUT_MIN ?= 10

## test-stress-simulate> run stress tests using simulated alerts (STRESS_ALERT_COUNT=100)
.PHONY: test-stress-simulate
test-stress-simulate:
	$(GINKGO) -v --label-filter="stress-simulate" ./test/integration/...

## help-integration> show integration testing workflow and related targets
.PHONY: help-integration
help-integration:
	@echo 'Integration Testing Workflow:'
	@echo ''
	@echo '  Prerequisites (requires oc login first):'
	@echo '    make proxy                      - port-forward to thanos-querier'
	@echo '    make install-integration-test-tools - install yq and promtool'
	@echo ''
	@echo '  Run tests:'
	@echo '    make deploy-integration         - deploy to cluster'
	@echo '    make test-integration           - run integration tests'
	@echo '    make test-stress-simulate       - run stress tests with simulated alerts'
	@echo '    make undeploy-integration       - cleanup deployment'
	@echo ''
	@echo '  Environment variables:'
	@echo '    CHA_IMAGE=$(CHA_IMAGE)'
	@echo '    MANIFESTS_PATH=$(MANIFESTS_PATH)'
	@echo '    DEPLOYMENT_NAME=$(DEPLOYMENT_NAME)'
	@echo '    NAMESPACE=$(NAMESPACE)'
	@echo '    STRESS_ALERT_COUNT=$(STRESS_ALERT_COUNT)'
	@echo '    STRESS_ALERT_TIMEOUT_MIN=$(STRESS_ALERT_TIMEOUT_MIN)'
	@echo '    STRESS_INCIDENT_TIMEOUT_MIN=$(STRESS_INCIDENT_TIMEOUT_MIN)'
	@echo ''
	@echo '  To test against COO deployment:'
	@echo '    eval $$(make env-coo)'
