FROM golang:1.27-alpine AS builder

ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY fetcher ./fetcher
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/fetcher ./fetcher

FROM alpine:3.24

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /out/fetcher /app/fetcher

ENTRYPOINT ["/app/fetcher"]
