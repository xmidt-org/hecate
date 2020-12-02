FROM docker.io/library/golang:1.14-alpine as builder

LABEL MAINTAINER="Jack Murdock <jack_murdock@comcast.com>"

WORKDIR /go/src/github.com/xmidt-org/hecate

ARG VERSION=unknown
ARG GITCOMMIT=unknown
ARG BUILDTIME=unknown

ADD https://github.com/geofffranks/spruce/releases/download/v1.25.2/spruce-linux-amd64 /usr/local/bin/spruce
RUN chmod +x /usr/local/bin/spruce

RUN apk add --no-cache --no-progress \
    ca-certificates \
    make \
    git \
    openssh \
    gcc \
    libc-dev \
    upx

COPY . .
RUN make build

FROM alpine:3.11.5

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/github.com/xmidt-org/hecate/hecate.yaml /hecate.yaml
COPY --from=builder /go/src/github.com/xmidt-org/hecate/hecate /hecate
COPY --from=builder /go/src/github.com/xmidt-org/hecate/deploy/Dockerfile /go/src/github.com/xmidt-org/hecate/NOTICE /go/src/github.com/xmidt-org/hecate/LICENSE /go/src/github.com/xmidt-org/hecate/CHANGELOG.md /
COPY --from=builder /go/src/github.com/xmidt-org/hecate/deploy/packaging/entrypoint.sh /entrypoint.sh
COPY --from=builder /go/src/github.com/xmidt-org/hecate/deploy/packaging/hecate_spruce.yaml /tmp/hecate_spruce.yaml
COPY --from=builder /usr/local/bin/spruce /spruce

RUN mkdir /etc/hecate/ && touch /etc/hecate/hecate.yaml && chmod 666 /etc/hecate/hecate.yaml

USER nobody

ENTRYPOINT ["/entrypoint.sh"]

EXPOSE 6701

CMD ["/hecate"]
