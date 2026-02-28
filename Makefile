.PHONY: all build test test-race lint clean

BINARY := zns-iscsi
CMD     := ./cmd/zns-iscsi

all: build

build:
	go build -o bin/$(BINARY) $(CMD)

test:
	go test ./...

test-race:
	go test -race ./...

test-verbose:
	go test -v -race ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; running go vet instead"; \
		go vet ./...; \
	fi

clean:
	rm -rf bin/

.PHONY: coverage
coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report written to coverage.html"
