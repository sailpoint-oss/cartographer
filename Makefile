# Cartographer
# =============================================================================
# Extraction-only OpenAPI tooling.

.PHONY: help build test extract-go extract-java extract-ts clean

.DEFAULT_GOAL := help

TOOL := cartographer/cartographer

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  = -X github.com/sailpoint-oss/cartographer/cmd.Version=$(VERSION) \
           -X github.com/sailpoint-oss/cartographer/cmd.Commit=$(COMMIT) \
           -X github.com/sailpoint-oss/cartographer/cmd.BuildDate=$(DATE)

help: ## Show this help message
	@echo "Cartographer"
	@echo "============"
	@echo ""
	@echo "Extraction-side OpenAPI tooling."
	@echo "Use Cartographer directly from a service repo, CI workflow, or local build."
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build the cartographer binary
	@echo "Building cartographer..."
	cd cartographer && go build -ldflags "$(LDFLAGS)" -o cartographer .
	@echo "Built: $(TOOL)"

test: ## Run cartographer unit tests
	cd cartographer && go test ./...

extract-go: build ## Extract a single Go service (usage: make extract-go ROOT=../svc TITLE="Service")
	$(TOOL) extract --lang go --root "$(ROOT)" --title "$(TITLE)" --output "$(notdir $(ROOT))-openapi.yaml"

extract-java: build ## Extract a single Java service (usage: make extract-java ROOT=../svc TITLE="Service")
	$(TOOL) extract --lang java --root "$(ROOT)" --title "$(TITLE)" --output "$(notdir $(ROOT))-openapi.yaml"

extract-ts: build ## Extract a single TypeScript service (usage: make extract-ts ROOT=../svc TITLE="Service")
	$(TOOL) extract --lang typescript --root "$(ROOT)" --title "$(TITLE)" --output "$(notdir $(ROOT))-openapi.yaml"

clean: ## Remove built binary
	rm -f cartographer/cartographer
