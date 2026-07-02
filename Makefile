SERVICES := shared delivery runtime observation gateway conversation timeline mcp visual-contract visual-inspect doc-publish artifacts migration integration
GOCACHE ?= $(CURDIR)/.gocache
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.golangci-lint-cache

DEN_MCP_SMOKE_SSH_HOST ?= den-srv

.PHONY: test build build-all lint mcp-smoke mcp-smoke-live mcp-smoke-live-den-srv

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
	$(error DEN_MCP_SMOKE_DEN_CORE_URL is required, for example http://127.0.0.1:5299 when running on den-srv)
endif
ifndef DEN_MCP_SMOKE_TASKS_URL
	$(error DEN_MCP_SMOKE_TASKS_URL is required, for example http://127.0.0.1:8092 when running on den-srv)
endif
ifndef DEN_MCP_SMOKE_DOCUMENTS_URL
	$(error DEN_MCP_SMOKE_DOCUMENTS_URL is required, for example http://127.0.0.1:8094 when running on den-srv)
endif
ifndef DEN_MCP_SMOKE_GUIDANCE_URL
	$(error DEN_MCP_SMOKE_GUIDANCE_URL is required, for example http://127.0.0.1:8097 when running on den-srv)
endif
	python3 mcp/scripts/hermes_smoke.py --mode live

mcp-smoke-live-den-srv:
	ssh $(DEN_MCP_SMOKE_SSH_HOST) 'cd /data/services/den-services && set -a && . /etc/den-services/mcp.env && set +a && DEN_MCP_SMOKE_DEN_CORE_URL=http://127.0.0.1:5299 DEN_MCP_SMOKE_TASKS_URL=http://127.0.0.1:8092 DEN_MCP_SMOKE_DOCUMENTS_URL=http://127.0.0.1:8094 DEN_MCP_SMOKE_GUIDANCE_URL=http://127.0.0.1:8097 DEN_MCP_SMOKE_READ_TASK_ID=$${DEN_MCP_SMOKE_READ_TASK_ID:-3446} make mcp-smoke-live'
