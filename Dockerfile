FROM docker.io/library/golang:1.23-bookworm AS builder

# git tag/shorthash passed on by jenkins
ARG DOCKER_IMAGE_VERSION
ENV DOCKER_IMAGE_VERSION=${DOCKER_IMAGE_VERSION}

WORKDIR /go/src/github.com/txsociety/spice-harvester/

COPY go.mod .
COPY go.sum .

RUN go mod download

# Copy the go source
COPY cmd cmd
COPY internal internal
COPY pkg pkg

COPY Makefile .

RUN apt-get update && \
    apt-get install -y libsodium23

# Build
RUN make build DOCKER_IMAGE_VERSION=$DOCKER_IMAGE_VERSION

FROM ubuntu:22.04 as spice-harvester-api

RUN mkdir -p /app/lib
RUN apt-get update && \
    apt-get install -y openssl ca-certificates libsecp256k1-0 libsodium23 wget && \
    rm -rf /var/lib/apt/lists/*
COPY --from=builder /go/pkg/mod/github.com/tonkeeper/tongo*/lib/linux /app/lib/
ENV LD_LIBRARY_PATH=/app/lib/
COPY --from=builder /go/src/github.com/txsociety/spice-harvester/bin/api .

ENTRYPOINT ["/api"]

FROM golang:alpine as ton-reverse-proxy

RUN apk update && apk add git build-base

RUN git clone https://github.com/txsociety/reverse-proxy.git /app

WORKDIR /app
RUN make build

ENTRYPOINT ["build/tonutils-reverse-proxy"]