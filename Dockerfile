ARG DEBIAN_IMAGE=debian:stable-slim

FROM --platform=$BUILDPLATFORM ${DEBIAN_IMAGE} AS build
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get -qq update \
    && apt-get -qq --no-install-recommends install libcap2-bin ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
# Create nonroot user/group records (uid/gid 65532) for the scratch runtime.
RUN groupadd -g 65532 -r nonroot && useradd -u 65532 -r -g nonroot nonroot
COPY coredns /coredns
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
