.PHONY: help init deps-update test tidy static lint lint-update vuln-check modernize outdated fmt vet check build clean install
.DEFAULT_GOAL := help

# Variables
BINARY_NAME := pdf2qti
BUILD_DIR   := bin
CMD         := ./cmd/pdf2qti

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## init: Initialize complete development environment (git hooks)
init:
	@echo "Initializing development environment..."
	@cp .githooks/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Development environment initialized ✓"

deps-update: lint-update
	@echo "Updating Go modules to latest versions..."
	@go get -u -t ./...
	@go mod tidy
	@echo "Go modules updated ✓"

## test: Run all tests with coverage
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt ./...

tidy:
	@echo "Tidying Go modules..."
	@go mod tidy
	@echo "Go modules tidied ✓"

## static: Run all linting tools
static: tidy vet lint vuln-check modernize
	@echo "All linting completed ✓"

## lint: Run golangci-lint with auto-fix enabled
lint:
	@echo "Running golangci-lint..."
	@go run -modfile=golangci-lint.mod github.com/golangci/golangci-lint/cmd/golangci-lint run --fix ./...

## lint-update: Update golangci-lint to latest version
lint-update:
	@echo "Updating golangci-lint..."
	@go get -modfile=golangci-lint.mod github.com/golangci/golangci-lint@latest

vuln-check:
	@echo "Checking for vulnerabilities..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

modernize:
	@echo "Running modernize analysis..."
	@go run golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest -fix -test ./...

outdated:
	@echo "Checking for outdated direct dependencies..."
	@go list -u -m -f '{{if not .Indirect}}{{.}}{{end}}' all 2>/dev/null | grep '\[' || echo "All direct dependencies are up to date"

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## check: Run all checks (format, vet, lint, test)
check: tidy fmt static test
	@echo "All checks completed ✓"

## build: Build the binary
build:
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD)

## clean: Remove build artifacts
clean:
	@rm -rf $(BUILD_DIR)/ out/ coverage.txt

## install: Install the binary
install:
	@go install $(CMD)
