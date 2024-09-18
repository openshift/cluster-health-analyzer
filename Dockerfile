FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-openshift-4.17 AS builder

WORKDIR /src
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd cmd
COPY pkg pkg
COPY main.go main.go

ENV GOOS=linux
ENV CGO_ENABLED=1
ENV GOFLAGS=-mod=readonly
ENV GOEXPERIMENT=strictfipsruntime
RUN go build -tags strictfipsruntime -o /bin/cluster-health-analyzer

FROM registry.ci.openshift.org/ocp/4.17:base-rhel9

WORKDIR /
COPY --from=builder /bin/cluster-health-analyzer /bin/cluster-health-analyzer
USER 65532:65532

ENTRYPOINT ["/bin/cluster-health-analyzer", "serve"]
