# SPDX-License-Identifier: Apache-2.0

.PHONY: build test race vet fmt-check check banner icon clean

# Chrome or Chromium, for 'make icon'.
CHROME ?= $(shell command -v google-chrome || command -v chromium || echo "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")

build: ## Build ./gh-ci-status (then 'gh extension install .' runs it as 'gh ci-status')
	go build -o gh-ci-status ./cmd/gh-ci-status

test: ## Run the tests
	go test -count=1 ./...

race: ## Run the tests with the race detector
	go test -race -count=1 ./...

vet: ## Run go vet
	go vet ./...

fmt-check: ## Fail if any file needs gofmt
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

check: fmt-check vet race ## Everything CI runs

banner: ## Regenerate assets/banner.svg and assets/icon.svg from the wordmark
	UPDATE_BANNER=1 go test -count=1 ./internal/banner -run 'TestBannerSVG|TestIconSVG'

icon: banner ## Make assets/icon.png (1024x1024) from assets/icon.svg with Chrome
	"$(CHROME)" --headless=new --disable-gpu --no-first-run --hide-scrollbars --window-size=512,512 \
		--force-device-scale-factor=2 --screenshot="$(CURDIR)/assets/icon.png" "file://$(CURDIR)/assets/icon.svg"

clean: ## Remove build output
	rm -f gh-ci-status
