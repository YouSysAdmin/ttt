# Used by GoReleaser: the prebuilt ttt binary is copied into the build
# context, no compilation happens here. See the dockers section of
# .goreleaser.yml.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && adduser -D -H ttt \
    && mkdir -p /data \
    && chown ttt /data

COPY ttt /usr/bin/ttt

USER ttt
VOLUME /data
EXPOSE 8320

# The token is required - pass TTT_SERVER_TOKEN (see docker-compose.yml).
ENTRYPOINT ["/usr/bin/ttt"]
CMD ["server", "--db", "/data/ttt.db"]
