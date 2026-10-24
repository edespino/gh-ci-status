# SPDX-License-Identifier: Apache-2.0

.PHONY: build test race vet fmt-check check banner clean

build: ## Build bin/ci-status
	go build -o bin/ci-status ./cmd/ci-status

test: ## Run the tests
	go test -count=1 ./...

race: ## Run the tests with the race detector
	go test -race -count=1 ./...

vet: ## Run go vet
	go vet ./...

fmt-check: ## Fail if any file needs gofmt
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt-check vet race ## Everything CI runs

banner: ## Regenerate assets/banner.svg from the wordmark
	UPDATE_BANNER=1 go test -count=1 ./internal/banner -run TestBannerSVG

clean: ## Remove build output
	rm -rf bin
