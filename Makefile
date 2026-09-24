.PHONY: build test lint tidy run clean

build:
	CGO_ENABLED=0 go build -o beavend ./cmd/beavend

test:
	CGO_ENABLED=0 go test -v ./...

lint:
	golangci-lint run ./...

tidy:
	GOTOOLCHAIN=local go mod tidy

run:
	CGO_ENABLED=0 go run ./cmd/beavend --role=all

clean:
	rm -f beavend
