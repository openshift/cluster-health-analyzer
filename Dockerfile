FROM golang:1.24.10 as builder
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
RUN go build -tags strictfipsruntime -o /bin/cluster-health-analyzer

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /
COPY --from=builder /bin/cluster-health-analyzer /bin/cluster-health-analyzer
USER 65532:65532

ENTRYPOINT ["/bin/cluster-health-analyzer"]
