#!/bin/bash

echo "Building ShadSpace Master Node..."

# Create bin directory if it doesn't exist
mkdir -p bin

# Build master node
go build -o bin/master-node ./cmd/master-node

if [ $? -eq 0 ]; then
    echo "Build successful! Master node binary: bin/master-node"
    ./bin/master-node
else
    echo "Build failed!"
    exit 1
fi