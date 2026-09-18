.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
GOVULNCHECK_VERSION := v1.8.0

.PHONY: help fmt fmt-check vet lint test build docs check vuln

help:
	@printf '%s\n' \
	  'make check      Run formatting, vet, tests, build, links, and whitespace checks' \
	  'make fmt        Format Go source' \
	  'make lint       Check Go formatting and run go vet' \
	  'make test       Run tests with the race detector' \
	  'make build      Compile all Go packages (tooling only at bootstrap)' \
	  'make docs       Check repository Markdown links and local heading anchors' \
	  'make vuln       Run pinned govulncheck (requires network access)'

fmt:
	$(GOFMT) -w $$(git ls-files --cached --others --exclude-standard -- '*.go')

fmt-check:
	@files="$$($(GOFMT) -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))" || exit $$?; \
	  test -z "$$files" || { printf 'Run make fmt for:\n%s\n' "$$files"; exit 1; }

vet:
	$(GO) vet ./...

lint: fmt-check vet

test:
	$(GO) test -race -count=1 ./...

build:
	$(GO) build -o bin/ ./...

docs:
	$(GO) run ./tools/checkdocs

check: lint test build docs
	git diff --check
	git diff --cached --check

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
