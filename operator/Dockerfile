FROM golang:1.26.5 AS builder

# The official golang images set GOTOOLCHAIN=local, so a base older than the
# `go` directive in operator/go.mod fails the build outright rather than
# fetching a toolchain. `auto` lets a future go.mod bump resolve itself against
# the checksum database instead of hard-failing here.
ENV GOTOOLCHAIN=auto

# CA certs + tzdata + nonroot user/group records for the scratch runtime.
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 65532 -r nonroot && useradd -u 65532 -r -g nonroot nonroot

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dns-operator ./cmd/operator/

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /workspace/dns-operator /dns-operator
USER 65532:65532
ENTRYPOINT ["/dns-operator"]
