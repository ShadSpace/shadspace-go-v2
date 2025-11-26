#!/bin/bash
# build-wasm.sh

# Build Go to WASM
GOOS=js GOARCH=wasm go build -o dist/shadspace.wasm cmd/wasm/main.go

# Copy Go WASM exec helper
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" dist/

echo "WASM build complete: dist/shadspace.wasm"