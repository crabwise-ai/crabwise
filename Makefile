VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/crabwise-ai/crabwise/internal/cli.Version=$(VERSION)"

.PHONY: build test bench-gate bench-sustained vet lint clean npm-stage npm-pack npm-publish npm-set-version npm-verify-binaries

build:
	go build $(LDFLAGS) -o bin/crabwise ./cmd/crabwise

test:
	go test -race -count=1 ./...

bench-gate:
	bash scripts/ci/check_m3_bench.sh

bench-sustained:
	go test -tags m3_bench -run 'TestProxySustainedLoad|TestEventLoss|TestQueueSaturation|TestSQLiteBatch' ./... -v -timeout 180s

vet:
	go vet ./...

lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "golangci-lint not installed, skipping"
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || true

clean:
	rm -rf bin/

npm-set-version:
	@test -n "$(VERSION)" || (echo "VERSION is required" >&2; exit 1)
	bash scripts/npm/set-version.sh "$(VERSION)"

npm-stage:
	@test -n "$(TAG)" || (echo "TAG is required" >&2; exit 1)
	bash scripts/npm/stage-release-binaries.sh "$(TAG)"

npm-verify-binaries:
	bash scripts/npm/verify-staged-binaries.sh

npm-pack: npm-verify-binaries
	npm pack --dry-run ./npm/platform/darwin-x64
	npm pack --dry-run ./npm/platform/darwin-arm64
	npm pack --dry-run ./npm/platform/linux-x64
	npm pack --dry-run ./npm/platform/linux-arm64
	npm pack --dry-run ./npm/crabwise

npm-publish: npm-verify-binaries
	npm publish --provenance --access public ./npm/platform/darwin-x64
	npm publish --provenance --access public ./npm/platform/darwin-arm64
	npm publish --provenance --access public ./npm/platform/linux-x64
	npm publish --provenance --access public ./npm/platform/linux-arm64
	npm publish --provenance --access public ./npm/crabwise
