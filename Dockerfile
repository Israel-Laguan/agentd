# Dockerfile - Build and run agentd
#
# Build: docker build -t agentd .
# Run:   docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd
#        docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd init
#        docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd start -v  # verbose
#
# The default target is the production image. `--target dev` is the devenv
# image (make dev-build): see the dev stage below.

FROM golang:1.26-alpine AS source
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM source AS builder
RUN CGO_ENABLED=0 go build -o /agentd ./cmd/agentd

# The fakeacp tag adds a scripted fake ACP agent and its preset (T-069).
FROM source AS dev-builder
RUN CGO_ENABLED=0 go build -tags fakeacp -o /agentd ./cmd/agentd

FROM alpine:3.21@sha256:f27cad9117495d32d067133afff942cb2dc745dfe9163e949f6bfe8a6a245339 AS base
RUN apk add --no-cache sqlite-libs bash
RUN adduser -D -s /bin/bash agentd
RUN mkdir -p /home/agentd/projects /home/agentd/uploads /home/agentd/archives && chown -R agentd:agentd /home/agentd
WORKDIR /home/agentd
ENTRYPOINT ["agentd"]
CMD ["init"]

# dev: agentd built with -tags fakeacp, plus bubblewrap, so an acp agent runs
# in the jail. The jail also needs the container started with
# --security-opt unmask=ALL (devenv/compose.yaml), or bubblewrap cannot mount
# /proc and agentd falls back to filter-only, which refuses acp agents.
FROM base AS dev
RUN apk add --no-cache bubblewrap
COPY --from=dev-builder /agentd /usr/local/bin/agentd
USER agentd

FROM base
COPY --from=builder /agentd /usr/local/bin/agentd
USER agentd
