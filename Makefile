REPO_DIR:=$(shell pwd)

.PHONY: update-generated
update-generated:
	go install -mod=readonly -modfile=hack/codegen/go.mod k8s.io/kube-openapi/cmd/openapi-gen
	$(GOPATH)/bin/openapi-gen \
		--output-pkg "github.com/openshift/cluster-health-analyzer/cmd/serve" \
		--logtostderr \
		--output-dir cmd/serve/ \
		--output-file zz_generated.openapi.go \
        -r /dev/null \
        k8s.io/apimachinery/pkg/apis/meta/v1 \
		k8s.io/apimachinery/pkg/version

