FROM alpine:3.22

RUN apk add --no-cache bash ca-certificates curl jq && \
    adduser -D -H -u 65532 sprout
COPY deploy/local_version_worker/snapshot-worker.sh /usr/local/bin/snapshot-worker
RUN chmod 0755 /usr/local/bin/snapshot-worker

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/snapshot-worker"]
