# syntax=docker/dockerfile:1
# Built by goreleaser: the binary is prebuilt on the host and copied in,
# this stage only assembles the final runtime image.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S nexus3 \
    && adduser -S -D -H -G nexus3 nexus3

COPY nexus3-go /usr/local/bin/nexus3-go

USER nexus3

ENTRYPOINT ["/usr/local/bin/nexus3-go"]
