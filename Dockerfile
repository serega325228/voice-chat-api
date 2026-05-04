FROM golang:1.26.2-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/migrator ./cmd/migrator

FROM alpine:3.22 AS api

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/api /app/api
COPY config /app/config

ENV CONFIG_PATH=/app/config/docker.yaml

EXPOSE 8082

ENTRYPOINT ["/app/api"]

FROM alpine:3.22 AS migrator

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/migrator /app/migrator
COPY migrations /app/migrations

ENTRYPOINT ["/app/migrator"]
