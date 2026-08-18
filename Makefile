.PHONY: help init update-deps update-tools test tidy check static lint update-lint vuln-check modernize outdated fmt vet build clean install
.DEFAULT_GOAL := help

# Variables
BINARY_NAME          := pdf2qti
BUILD_DIR            := bin
CMD                  := .

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'

## init: Initialize complete development environment (git hooks)
init:
	@echo "Initializing development environment..."
	@mkdir -p .git/hooks
	@cp .githooks/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Development environment initialized ✓"

update-deps:
	@echo "Updating Go modules to latest versions..."
	@go get -u -t ./...
	@go mod tidy
	@echo "Go modules updated ✓"

## test: Run all tests with coverage
test:
	@echo "Running tests..."
	@go tool -modfile=tools/go.mod gotestsum -- -race -shuffle=on -coverprofile=coverage.txt ./...

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
	@go tool -modfile=tools/golangci-lint/go.mod golangci-lint version
	@go tool -modfile=tools/golangci-lint/go.mod golangci-lint run --fix ./...

## update-lint: Update golangci-lint to latest version
update-lint:
	@echo "Updating golangci-lint..."
	@go get -tool -modfile=tools/golangci-lint/go.mod github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

## update-tools: Update tools/go.mod managed tools
update-tools:
	@echo "Updating tools/go.mod managed tools..."
	@go get -tool -modfile=tools/go.mod gotest.tools/gotestsum@latest
	@go get -tool -modfile=tools/go.mod golang.org/x/vuln/cmd/govulncheck@latest
	@go get -tool -modfile=tools/go.mod golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@latest
	@go mod edit -modfile=tools/go.mod -droprequire github.com/alecthomas/kong || true

vuln-check:
	@echo "Checking for vulnerabilities..."
	@go tool -modfile=tools/go.mod govulncheck -version
	@go tool -modfile=tools/go.mod govulncheck ./...

modernize:
	@echo "Running modernize analysis..."
	@go tool -modfile=tools/go.mod modernize -V=full
	@go tool -modfile=tools/go.mod modernize -fix -test ./...

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
