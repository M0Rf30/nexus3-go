# syntax=docker/dockerfile:1
# Built by goreleaser (dockers_v2): the per-platform binaries are prebuilt on
# the host and laid out as <os>/<arch>/nexus3-go in the build context, this
# stage only assembles the final runtime image.
FROM alpine:3.24

RUN apk add --no-cache ca-certificates \
    && addgroup -S nexus3 \
    && adduser -S -D -H -G nexus3 nexus3

ARG TARGETPLATFORM
COPY $TARGETPLATFORM/nexus3-go /usr/local/bin/nexus3-go

USER nexus3

ENTRYPOINT ["/usr/local/bin/nexus3-go"]
