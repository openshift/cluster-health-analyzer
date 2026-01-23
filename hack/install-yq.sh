#!/bin/bash

set -e

# Version to install
YQ_VERSION="${YQ_VERSION:-4.44.6}"

# Determine install directory
if [ -z "${GOPATH:-}" ]; then
    eval "$(go env | grep GOPATH)"
fi
INSTALL_DIR="${INSTALL_DIR:-${GOPATH}/bin}"

# Check if yq is already installed in the target location
if [ -x "${INSTALL_DIR}/yq" ]; then
    echo "yq already installed at ${INSTALL_DIR}/yq"
    "${INSTALL_DIR}/yq" --version
    exit 0
fi

# Check if yq is available in PATH
if command -v yq &> /dev/null; then
    echo "yq already available in PATH"
    yq --version
    exit 0
fi

echo "Installing yq v${YQ_VERSION} to ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case ${ARCH} in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

curl -sSfL "https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_${OS}_${ARCH}" -o "${INSTALL_DIR}/yq"
chmod +x "${INSTALL_DIR}/yq"

echo "yq installed successfully"
"${INSTALL_DIR}/yq" --version
