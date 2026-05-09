# Dockerfile - Build and run agentd
#
# Build: docker build -t agentd .
# Run:   docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd
#        docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd init
#        docker run --rm -v $(pwd)/.agentd:/home/agentd/.agentd agentd start -v  # verbose

FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /agentd ./cmd/agentd

FROM alpine:latest
RUN apk add --no-cache sqlite-libs bash
COPY --from=builder /agentd /usr/local/bin/agentd
RUN adduser -D -s /bin/bash agentd
USER agentd
WORKDIR /home/agentd
ENTRYPOINT ["agentd"]
CMD ["init"]