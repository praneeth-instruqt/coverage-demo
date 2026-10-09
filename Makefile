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
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	echo "Total coverage: $$total% (threshold $(COVERAGE_THRESHOLD)%)"; \
	awk -v t=$$total -v min=$(COVERAGE_THRESHOLD) 'BEGIN { exit (t < min) }'

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
