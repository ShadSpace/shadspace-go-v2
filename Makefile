.PHONY: build-wasm build-js clean serve setup

# Setup: Get WASM executor
setup:
	@echo "Setting up WASM environment..."
	cp "$(shell go env GOROOT)/misc/wasm/wasm_exec.js" dist/
	@echo "WASM setup complete"

# Build WASM target
build-wasm:
	@echo "Building WebAssembly module..."
	GOOS=js GOARCH=wasm go build -tags=js,wasm -o dist/shadspace.wasm cmd/wasm/main.go
	@echo "WASM build complete: dist/shadspace.wasm"

# Copy JS files
build-js: build-wasm
	@echo "Copying JavaScript files..."
	cp pkg/js/wasm-loader.js dist/
	cp pkg/js/shadspace-sdk.js dist/
	@echo "JavaScript files copied"

# Build everything
build-all: build-wasm build-js
	@echo "Full build complete"

# Clean build artifacts
clean:
	rm -rf dist/*

# Serve for testing
serve:
	@echo "Starting test server on http://localhost:8080"
	cd dist && python3 -m http.server 8080

# Development build
dev: build-all serve