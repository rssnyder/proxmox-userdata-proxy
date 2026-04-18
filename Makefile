.PHONY: all build test test-verbose test-coverage lint fmt vet clean run docker-build docker-run help api-health api-test-vm api-resize-vm api-delete-vm

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# Binary name
BINARY_NAME=proxmox-userdata-proxy
BINARY_PATH=bin/$(BINARY_NAME)

# Docker parameters
DOCKER_IMAGE=proxmox-userdata-proxy
DOCKER_TAG=latest

# Build flags
LDFLAGS=-ldflags "-w -s"

all: test build

## Build

build: ## Build the binary
	@echo "Building..."
	@mkdir -p bin
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/proxy

build-linux: ## Build for Linux (useful for cross-compilation)
	@echo "Building for Linux..."
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH)-linux-amd64 ./cmd/proxy

## Test

test: ## Run tests
	$(GOTEST) -race ./...

test-verbose: ## Run tests with verbose output
	$(GOTEST) -race -v ./...

test-coverage: ## Run tests with coverage report
	$(GOTEST) -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-coverage-func: ## Show coverage by function
	$(GOTEST) -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out

## Code Quality

lint: ## Run linter (requires golangci-lint)
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

fmt: ## Format code
	$(GOFMT) ./...

vet: ## Run go vet
	$(GOVET) ./...

check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)

## Dependencies

deps: ## Download dependencies
	$(GOMOD) download

tidy: ## Tidy dependencies
	$(GOMOD) tidy

## Run

run: build ## Build and run the binary
	@echo "Running... (set environment variables first)"
	./$(BINARY_PATH)

run-dev: ## Run with go run (faster for development)
	$(GOCMD) run ./cmd/proxy

## Docker

docker-build: ## Build Docker image
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

docker-run: docker-build ## Build and run Docker container (requires env vars)
	@echo "Running Docker container..."
	@echo "Make sure to set required environment variables!"
	docker run --rm -it \
		-p 8443:8443 \
		-e PROXMOX_URL \
		-e PROXMOX_INSECURE \
		-e SNIPPET_STORAGE_PATH=/var/lib/vz \
		-v /var/lib/vz:/var/lib/vz \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

docker-push: docker-build ## Push Docker image (set DOCKER_REGISTRY first)
	@if [ -z "$(DOCKER_REGISTRY)" ]; then echo "Set DOCKER_REGISTRY first"; exit 1; fi
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)

## API Testing

PROXY_URL ?= http://localhost:8443
PROXMOX_AUTH ?= $(PVE_AUTH)

api-health: ## Check proxy health endpoint
	curl -s $(PROXY_URL)/health | jq .

define CLOUD_INIT_YAML
#cloud-config
package_upgrade: true
packages:
  - qemu-guest-agent
runcmd:
  - systemctl enable qemu-guest-agent
  - systemctl start qemu-guest-agent
endef
export CLOUD_INIT_YAML

api-test-vm: ## Create a test VM via the proxy (uses PVE_AUTH env var)
	@if [ -z "$(PROXMOX_AUTH)" ]; then echo "Error: Set PVE_AUTH env var (e.g., PVEAPIToken=user@pve!token=secret)"; exit 1; fi
	@curl -X POST "$(PROXY_URL)/api2/json/nodes/pve0/qemu" \
		-H "Authorization: $(PROXMOX_AUTH)" \
		-H "Content-Type: application/x-www-form-urlencoded" \
		-d "vmid=9999" \
		-d "name=test-cloud-init" \
		-d "memory=1024" \
		-d "cores=1" \
		-d "sockets=1" \
		-d "cpu=host" \
		-d "ostype=l26" \
		-d "agent=1" \
		-d "scsihw=virtio-scsi-pci" \
		-d "scsi0=data:0,import-from=baelor:import/debian-13-genericcloud-amd64.qcow2" \
		-d "ide2=data:cloudinit" \
		-d "boot=order=scsi0" \
		-d "net0=virtio,bridge=vmbr0" \
		-d "ipconfig0=ip=dhcp" \
		-d "serial0=socket" \
		-d "vga=serial0" \
		-d "cloudinit_storage=baelor" \
		--data-urlencode "cloudinit_userdata=$$CLOUD_INIT_YAML" \
		| jq .

api-resize-vm: ## Resize the test VM disk to 10GB
	@curl -X PUT "$(PROXY_URL)/api2/json/nodes/pve0/qemu/9999/resize" \
		-H "Authorization: $(PROXMOX_AUTH)" \
		-d "disk=scsi0" \
		-d "size=10G" \
		| jq .

api-delete-vm: ## Delete the test VM
	@curl -X DELETE "$(PROXY_URL)/api2/json/nodes/pve0/qemu/9999" \
		-H "Authorization: $(PROXMOX_AUTH)" \
		| jq .

## Utilities

clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -rf bin/
	rm -f coverage.out coverage.html

install-tools: ## Install development tools
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## Help

help: ## Show this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
