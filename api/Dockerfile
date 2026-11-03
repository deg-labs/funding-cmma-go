FROM golang:1.27-alpine AS builder

ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/api ./api

FROM alpine:3.24

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /out/api /app/api

EXPOSE 8000

ENTRYPOINT ["/app/api"]
