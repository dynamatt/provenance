SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c

MODULE := github.com/dynamatt/provenance
BIN    := bin/provenance

# Build identity is injected, never read at runtime. Override on the command
# line (make build VERSION=v0.1.0) for release builds.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

# Reproducible: no cgo, no absolute paths (-trimpath), no VCS stamping beyond
# the two values above (-buildvcs=false), and nothing time-dependent in ldflags.
GO_BUILD_FLAGS := -trimpath -buildvcs=false
GO_LDFLAGS     := -X $(MODULE)/internal/version.Version=$(VERSION) \
                  -X $(MODULE)/internal/version.Commit=$(COMMIT)

EXAMPLE_REPO ?= https://github.com/dynamatt/provenance-example.git
# The branch bump-example pins: main, or an epic branch while the epic is open.
EXAMPLE_BRANCH ?= main
EXAMPLE_DIR  := .cache/example
EXAMPLE_REF  := $(shell cat testdata/example-repo.ref)

# Snapshot of `export website` on the pinned example (E1 standing acceptance).
GOLDEN_DIR := testdata/golden
VALIDATE_DIR := testdata/validate
# One example of every validate failure, and of its passing counterpart.
VIOLATIONS_DIR := testdata/violations

# provenance-website checkout that receives the generated CLI reference.
WEBSITE_DIR ?= ../provenance-website

.PHONY: build dist test lint acceptance golden-update example bump-example docs ci

build:
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags '$(GO_LDFLAGS)' -o $(BIN) ./cmd/provenance

# Cross-compile the release platforms into dist/ with exactly the build recipe
# above, and write the release manifest dist/SHA256SUMS (DES-0025). The
# reproducible-build workflow runs this on two runners and compares; the
# release workflow publishes its output.
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
	cd dist && LC_ALL=C sha256sum -- provenance-* | LC_ALL=C sort -k2 > SHA256SUMS

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

# Move the pin to the current tip of provenance-example's EXAMPLE_BRANCH.
bump-example:
	@sha=$$(git ls-remote $(EXAMPLE_REPO) refs/heads/$(EXAMPLE_BRANCH) | cut -f1); \
	if [ -z "$$sha" ]; then echo "could not read $(EXAMPLE_BRANCH) from $(EXAMPLE_REPO)"; exit 1; fi; \
	echo "$$sha" > testdata/example-repo.ref; \
	echo "example pinned to $$sha"

acceptance: build example
	PROV=$(CURDIR)/$(BIN) EXAMPLE_DIR=$(CURDIR)/$(EXAMPLE_DIR) GOLDEN_DIR=$(CURDIR)/$(GOLDEN_DIR) \
		VALIDATE_DIR=$(CURDIR)/$(VALIDATE_DIR) VIOLATIONS_DIR=$(CURDIR)/$(VIOLATIONS_DIR) bash scripts/acceptance.sh

# SARIF names the binary's version, which changes with every commit; the
# golden reports say dev instead.
SARIF_DEV := sed -E '/"name": "provenance",/{n;s/"version": "[^"]*"/"version": "dev"/;}'

# --output flags writing every validate format to $(1).<extension>.
validate_outputs = $(foreach f,text:txt json:json junit:junit.xml sarif:sarif,--output $(word 1,$(subst :, ,$(f)))=$(1).$(word 2,$(subst :, ,$(f))))

# Rewrite the golden site and the clean validate report from the pinned
# example, and the reports of testdata/violations. Commit the result with the
# change that caused it, so the PR shows the difference. The example must
# validate without errors; the violations repositories must fail (exit 1).
golden-update: build example
	rm -rf $(GOLDEN_DIR)
	cd $(EXAMPLE_DIR) && $(CURDIR)/$(BIN) export website --out $(CURDIR)/$(GOLDEN_DIR) >/dev/null
	mkdir -p $(VALIDATE_DIR)
	cd $(EXAMPLE_DIR) && $(CURDIR)/$(BIN) validate $(call validate_outputs,$(CURDIR)/$(VALIDATE_DIR)/clean)
	for r in core broken-schema; do \
		$(BIN) validate --path $(VIOLATIONS_DIR)/$$r $(call validate_outputs,$(VALIDATE_DIR)/violations-$$r) || [ $$? -eq 1 ]; \
	done
	for f in $(VALIDATE_DIR)/*.sarif; do $(SARIF_DEV) $$f > $$f.tmp && mv $$f.tmp $$f; done
	@echo "golden site and report rewritten: review 'git diff $(GOLDEN_DIR) $(VALIDATE_DIR)'"

# Regenerate the website's CLI reference from the command definitions. Run it
# whenever a command, flag or help text changes, and commit the result in
# provenance-website.
docs:
	go run ./tools/gendocs -out $(WEBSITE_DIR)/content/docs/cli

ci: lint test build acceptance
