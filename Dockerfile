ARG DEBIAN_IMAGE=debian:stable-slim
ARG GO_IMAGE=ghcr.io/hanzoai/mirror/golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125

# The binary is compiled here, for the platform being built: the image is a pure function of
# the commit, and a build needs no prepared input beside the source.
FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS compile
ARG TARGETOS TARGETARCH
# git: luxfi/crypto is not on the public module proxy, so Go resolves it from its repository,
# which github.com refuses without a credential and the forge serves openly; go.sum pins the hash.
RUN apk add --no-cache git \
    && git config --global url."https://git.hanzo.ai/luxfi/".insteadOf "https://github.com/luxfi/"
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags "grpcnotrace metrics" -trimpath -ldflags="-s -w" -o /coredns .

FROM --platform=$BUILDPLATFORM ${DEBIAN_IMAGE} AS build
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get -qq update \
    && apt-get -qq --no-install-recommends install libcap2-bin ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
# Create nonroot user/group records (uid/gid 65532) for the scratch runtime.
RUN groupadd -g 65532 -r nonroot && useradd -u 65532 -r -g nonroot nonroot
COPY --from=compile /coredns /coredns
RUN setcap cap_net_bind_service=+ep /coredns

# Scratch runtime — coredns is a static Go binary; bind capability on
# port 53 is set on the binary itself via setcap in the build stage and
# preserved by COPY (the file capability xattr survives the layer copy).
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group
COPY --from=build /coredns /coredns
USER 65532:65532
WORKDIR /
EXPOSE 53 53/udp
ENTRYPOINT ["/coredns"]
