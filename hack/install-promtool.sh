#!/bin/bash

set -e

# Version to install
PROMETHEUS_VERSION="${PROMETHEUS_VERSION:-3.2.1}"

# Determine install directory
if [ -z "${GOPATH:-}" ]; then
    eval "$(go env | grep GOPATH)"
fi
INSTALL_DIR="${INSTALL_DIR:-${GOPATH}/bin}"

# Check if promtool is already installed in the target location
if [ -x "${INSTALL_DIR}/promtool" ]; then
    echo "promtool already installed at ${INSTALL_DIR}/promtool"
    "${INSTALL_DIR}/promtool" --version
    exit 0
fi

# Check if promtool is available in PATH
if command -v promtool &> /dev/null; then
    echo "promtool already available in PATH"
    promtool --version
    exit 0
fi

echo "Installing promtool v${PROMETHEUS_VERSION} to ${INSTALL_DIR}..."
mkdir -p "${INSTALL_DIR}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case ${ARCH} in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

# Create temp directory for extraction
TMPDIR=$(mktemp -d)
trap "rm -rf ${TMPDIR}" EXIT

# Download and extract
curl -sSfL "https://github.com/prometheus/prometheus/releases/download/v${PROMETHEUS_VERSION}/prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}.tar.gz" -o "${TMPDIR}/prometheus.tar.gz"
tar xzf "${TMPDIR}/prometheus.tar.gz" -C "${TMPDIR}"
mv "${TMPDIR}/prometheus-${PROMETHEUS_VERSION}.${OS}-${ARCH}/promtool" "${INSTALL_DIR}/promtool"
chmod +x "${INSTALL_DIR}/promtool"

echo "promtool installed successfully"
"${INSTALL_DIR}/promtool" --version
