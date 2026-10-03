.PHONY: build test lint fmt vet

build:
	go build -o bin/snow ./...

test:
	go test ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...
