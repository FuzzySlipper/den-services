SERVICES := shared delivery runtime observation gateway conversation timeline visual-contract migration integration
GOCACHE ?= $(CURDIR)/.gocache
GOLANGCI_LINT_CACHE ?= $(CURDIR)/.golangci-lint-cache

.PHONY: test build build-all lint

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
