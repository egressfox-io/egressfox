.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt
GOVULNCHECK_VERSION := v1.8.0
ENVTEST_VERSION := 1.37.0
HELM ?= helm
DOCKER ?= docker
IMG ?= egressfox:dev

.PHONY: help fmt fmt-check vet lint test test-envtest build generate manifests generate-check helm-check docker-build e2e-kind docs check vuln

help:
	@printf '%s\n' \
	  'make check      Run formatting, vet, tests, build, links, and whitespace checks' \
	  'make fmt        Format Go source' \
	  'make lint       Check Go formatting and run go vet' \
	  'make test       Run tests with the race detector' \
	  'make test-envtest Run API/controller tests against pinned Kubernetes 1.37 envtest' \
	  'make generate   Regenerate Kubernetes deepcopy code and CRDs/RBAC' \
	  'make helm-check Lint and render the Helm chart for Kubernetes 1.37' \
	  'make e2e-kind   Run the required kind lifecycle and RBAC suite' \
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

test-envtest:
	@assets="$$(./hack/setup-envtest.sh)"; KUBEBUILDER_ASSETS="$$assets" $(GO) test -race -count=1 ./internal/controller/...

generate:
	$(GO) tool controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...

manifests:
	$(GO) tool controller-gen crd paths=./api/... output:crd:artifacts:config=config/crd/bases
	$(GO) tool controller-gen rbac:roleName=egressfox-manager-role paths=./internal/controller output:rbac:artifacts:config=config/rbac
	cp config/crd/bases/*.yaml charts/egressfox/crds/

generate-check: generate manifests
	git diff --exit-code -- api config/crd/bases config/rbac/role.yaml charts/egressfox/crds

helm-check:
	$(HELM) lint charts/egressfox --kube-version $(ENVTEST_VERSION)
	$(HELM) template egressfox charts/egressfox --namespace egressfox-system --kube-version $(ENVTEST_VERSION) >/dev/null

docker-build:
	$(DOCKER) build -t $(IMG) .

e2e-kind:
	./hack/e2e-kind.sh

build:
	$(GO) build -o bin/ ./...

docs:
	$(GO) run ./tools/checkdocs

check: generate-check lint test build docs helm-check
	git diff --check
	git diff --cached --check

vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
