.PHONY: all build clean test lint fmt sqlc-generate docker-build help

# Variables
ROTATOR_BIN=cmd/rotator/rotator
CONNECTOR_BIN=cmd/connector/vpn-rotator
DOCKER_IMAGE_ROTATOR=vpn-rotator-service
DOCKER_IMAGE_CONNECTOR=vpn-rotator-cli

all: build

## build: Build both binaries
build:
	@echo "Building rotator service..."
	cd cmd/rotator && go build -o rotator
	@echo "Building connector CLI..."
	cd cmd/connector && go build -o vpn-rotator
	@echo "Build complete"

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(ROTATOR_BIN) $(CONNECTOR_BIN)
	rm -rf dist/ build/
	@echo "✓ Clean complete"

## test: Run tests
test:
	@echo "Running tests..."
	go test -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -html=coverage.txt -o coverage.html
	@echo "✓ Coverage report: coverage.html"

## lint: Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	gofmt -s -w .

## sqlc-generate: Generate database code
sqlc-generate:
	@echo "Generating database code..."
	sqlc generate -f db/sqlc.yaml
	@echo "Creating symlink for internal db schema..."
	ln -s db/schema.sql internal/rotator/db/schema.sql

## docker-build: Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build -f deployments/docker/Dockerfile.rotator -t $(DOCKER_IMAGE_ROTATOR) .
	docker build -f deployments/docker/Dockerfile.connector -t $(DOCKER_IMAGE_CONNECTOR) .

## docker-run: Run service in Docker
docker-run:
	docker run --rm -it \
		-p 8080:8080 \
		-v $(PWD)/data:/data \
		-e HETZNER_API_TOKEN=${HETZNER_API_TOKEN} \
		$(DOCKER_IMAGE_ROTATOR)

## install-tools: Install development tools
install-tools:
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## help: Show this help
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.DEFAULT_GOAL := help
