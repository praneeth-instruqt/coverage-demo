.PHONY: run build test cover lint fmt vet docker clean

BIN := bin/todo-server
COVERAGE_THRESHOLD ?= 85

run:
	JWT_SECRET=$${JWT_SECRET:-dev-secret-change-me-dev-secret-change-me} go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/server

test:
	go test -race ./...

cover:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	go run ./tools/coverage -threshold $(COVERAGE_THRESHOLD) -report COVERAGE.md

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

fmt:
	gofmt -w .

vet:
	go vet ./...

docker:
	docker build -t todo-server .

clean:
	rm -rf bin coverage.out coverage.html
