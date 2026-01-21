#!/bin/bash

set -ex

# setting GOPATH if not set
if [ -z "${GOPATH:-}" ]; then
    eval "$(go env | grep GOPATH)"
fi

if [ ! -f "$GOPATH/bin/golangci-lint" ]
then
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.8.0
fi
