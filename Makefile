SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

MODULE := github.com/dynamatt/provenance
BIN    := bin/provenance

# Build identity is injected, never read at runtime. Override on the command
# line (make build VERSION=v0.1.0) for release builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)-$(shell date +%s%N)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

# Reproducible: no cgo, no absolute paths (-trimpath), no VCS stamping beyond
# the two values above (-buildvcs=false), and nothing time-dependent in ldflags.
GO_BUILD_FLAGS := -trimpath -buildvcs=false
GO_LDFLAGS     := -X $(MODULE)/internal/version.Version=$(VERSION) \
                  -X $(MODULE)/internal/version.Commit=$(COMMIT)

EXAMPLE_REPO ?= https://github.com/dynamatt/provenance-example.git
EXAMPLE_DIR  := .cache/example
EXAMPLE_REF  := $(shell cat testdata/example-repo.ref)

# provenance-website checkout that receives the generated CLI reference.
WEBSITE_DIR ?= ../provenance-website

.PHONY: build dist test lint acceptance example bump-example docs ci

build:
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags '$(GO_LDFLAGS)' -o $(BIN) ./cmd/provenance

# Cross-compile the release platforms into dist/ with exactly the build recipe
# above. The reproducible-build workflow runs this on two runners and compares.
PLATFORMS ?= linux/amd64 darwin/arm64 windows/amd64

dist:
	rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		out=dist/provenance-$$os-$$arch$$ext; \
		echo "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GO_BUILD_FLAGS) -ldflags '$(GO_LDFLAGS)' -o $$out ./cmd/provenance; \
	done

test:
	go test ./...

lint:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed on:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go tool staticcheck ./...

# Check out provenance-example at the pinned commit. Full history is kept:
# later features (last-changed SHA, revision history) read it.
example:
	@if [ ! -d $(EXAMPLE_DIR)/.git ]; then \
		git clone -q $(EXAMPLE_REPO) $(EXAMPLE_DIR); \
	elif ! git -C $(EXAMPLE_DIR) cat-file -e $(EXAMPLE_REF)^{commit} 2>/dev/null; then \
		git -C $(EXAMPLE_DIR) fetch -q origin; \
	fi
	git -C $(EXAMPLE_DIR) checkout -q --force --detach $(EXAMPLE_REF)
	git -C $(EXAMPLE_DIR) clean -q -fdx
	@echo "example: $(EXAMPLE_DIR) at $(EXAMPLE_REF)"

# Move the pin to the current tip of provenance-example's main branch.
bump-example:
	@sha=$$(git ls-remote $(EXAMPLE_REPO) refs/heads/main | cut -f1); \
	if [ -z "$$sha" ]; then echo "could not read main from $(EXAMPLE_REPO)"; exit 1; fi; \
	echo "$$sha" > testdata/example-repo.ref; \
	echo "example pinned to $$sha"

acceptance: build example
	PROV=$(CURDIR)/$(BIN) EXAMPLE_DIR=$(CURDIR)/$(EXAMPLE_DIR) bash scripts/acceptance.sh

# Regenerate the website's CLI reference from the command definitions. Run it
# whenever a command, flag or help text changes, and commit the result in
# provenance-website.
docs:
	go run ./tools/gendocs -out $(WEBSITE_DIR)/content/docs/cli

ci: lint test build acceptance
