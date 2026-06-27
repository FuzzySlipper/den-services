SERVICES := shared delivery runtime observation gateway conversation timeline mcp visual-contract visual-inspect doc-publish artifacts migration integration
GOCACHE ?= $(CURDIR)/.gocache
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.golangci-lint-cache

.PHONY: test build build-all lint mcp-smoke mcp-smoke-live

test:
	GOCACHE=$(GOCACHE) go test ./...
	@for service in $(SERVICES); do \
		echo "testing $$service"; \
		GOCACHE=$(GOCACHE) go test ./$$service/...; \
	done

build:
ifndef SERVICE
	$(error SERVICE is required, for example: make build SERVICE=gateway)
endif
	GOCACHE=$(GOCACHE) go build ./$(SERVICE)/...

build-all:
	@for service in $(SERVICES); do \
		echo "building $$service"; \
		GOCACHE=$(GOCACHE) go build ./$$service/...; \
	done

lint:
	GOCACHE=$(GOCACHE) GOLANGCI_LINT_CACHE=$(GOLANGCI_LINT_CACHE) golangci-lint run ./...

mcp-smoke:
	python3 mcp/scripts/hermes_smoke.py --mode local

mcp-smoke-live:
ifndef DEN_MCP_SMOKE_DEN_CORE_URL
	$(error DEN_MCP_SMOKE_DEN_CORE_URL is required, for example http://192.168.1.10:5199)
endif
	python3 mcp/scripts/hermes_smoke.py --mode live
