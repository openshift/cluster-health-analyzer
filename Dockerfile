# Build the binary
FROM docker.io/golang:1.22.1 as builder
WORKDIR /src
# Download and cache go modules before building.
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy go sources and build
COPY cmd cmd
COPY pkg pkg
COPY main.go main.go
RUN CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=readonly go build -o /bin/cluster-health-analyzer

FROM registry.access.redhat.com/ubi9-minimal:9.4
WORKDIR /

COPY --from=builder /bin/cluster-health-analyzer /bin/cluster-health-analyzer
USER 65532:65532
ENTRYPOINT ["/bin/cluster-health-analyzer", "serve"]