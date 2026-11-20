FROM golang:1.27 as builder
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd cmd
COPY pkg pkg
COPY main.go main.go

ENV GOOS=${TARGETOS:-linux}
# GOARCH has no default, so the binary builds for the host. On Apple M1, BUILDPLATFORM is set to 
# linux/arm64; on Apple x86, it's linux/amd64. Leaving it empty ensures the container and binary 
# match the host platform.
ENV GOARCH=${TARGETARCH}
ENV CGO_ENABLED=1
ENV GOFLAGS=-mod=readonly
# BUILD_TAGS=testonly additionally includes the dev-only debug endpoints
# (see docs/DEBUG_ENDPOINTS.md).
ARG BUILD_TAGS=strictfipsruntime
RUN go build -tags ${BUILD_TAGS} \
    -ldflags "-X github.com/openshift/cluster-health-analyzer/cmd.Version=dev-local" \
    -o /bin/cluster-health-analyzer

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /
COPY --from=builder /bin/cluster-health-analyzer /bin/cluster-health-analyzer
USER 65532:65532

ENTRYPOINT ["/bin/cluster-health-analyzer"]
