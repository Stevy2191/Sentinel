BINARY_NAME := sentinel
BIN_DIR     := bin
MAIN_PKG    := ./cmd/sentinel

.PHONY: build run dev test tidy clean

## build: Compile the binary into $(BIN_DIR)/$(BINARY_NAME)
build:
	go build -o $(BIN_DIR)/$(BINARY_NAME) $(MAIN_PKG)

## run: Run the server locally
run:
	go run $(MAIN_PKG)

## dev: Run with hot reload (requires 'air'; falls back to 'make run')
dev:
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "air not found; install with 'go install github.com/air-verse/air@latest'. Falling back to 'go run'."; \
		$(MAKE) run; \
	fi

## test: Run the test suite
test:
	go test ./...

## tidy: Sync go.mod/go.sum with source imports
tidy:
	go mod tidy

## clean: Remove build artifacts
clean:
	rm -rf $(BIN_DIR)
