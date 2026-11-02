.PHONY: build test fmt vet run-api run-fetcher

build:
	go build -o bin/funding-cmma-api ./api
	go build -o bin/funding-cmma-fetcher ./fetcher

test:
	go test ./...

race:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

run-api:
	DB_PATH=./data/cmma.db HTTP_ADDR=:8000 go run ./api

run-fetcher:
	DB_PATH=./data/cmma.db go run ./fetcher
