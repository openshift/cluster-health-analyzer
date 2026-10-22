#!/bin/bash

set -ex

# setting GOBIN / GOPATH if not set
if [ -z "${GOBIN:-}" ]; then
    eval "$(go env | grep GOBIN)"
fi
if [ -z "${GOPATH:-}" ]; then
    eval "$(go env | grep GOPATH)"
fi

INSTALL_DIR="${GOBIN:-$GOPATH/bin}"

if [ ! -f "$INSTALL_DIR/govulncheck" ]
then
    GOBIN="$INSTALL_DIR" go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
fi
