VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/crabwise-ai/crabwise/internal/cli.Version=$(VERSION)"

.PHONY: build test vet lint clean

build:
	go build $(LDFLAGS) -o bin/crabwise ./cmd/crabwise

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "golangci-lint not installed, skipping"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || true

clean:
	rm -rf bin/
