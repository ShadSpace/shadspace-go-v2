#!/bin/bash

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to display usage
usage() {
    echo "Usage: $0 [master|farmer|all|help]"
    echo ""
    echo "Options:"
    echo "  master    Build only the master node"
    echo "  farmer    Build only the farmer node" 
    echo "  all       Build both master and farmer nodes (default)"
    echo "  help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0              # Build both nodes (default)"
    echo "  $0 master       # Build only master node"
    echo "  $0 farmer       # Build only farmer node"
    echo "  $0 all          # Build both nodes"
    echo "  $0 help         # Show this help"
}

# Function to build master node
build_master() {
    print_status "Building master node..."
    go build -o bin/master-node ./cmd/master-node
    if [ $? -eq 0 ]; then
        print_success "Master node built successfully: bin/master-node"
        clear
        ./bin/master-node 
    else
        print_error "Failed to build master node"
        return 1
    fi
}

# Function to build farmer node
build_farmer() {
    print_status "Building farmer node..."
    go build -o bin/farmer-node ./cmd/farmer-node
    if [ $? -eq 0 ]; then
        print_success "Farmer node built successfully: bin/farmer-node"
        clear
        ./bin/farmer-node
    else
        print_error "Failed to build farmer node"
        return 1
    fi
}

# Function to build both nodes
build_all() {
    print_status "Building all ShadSpace nodes..."
    
    build_master
    if [ $? -ne 0 ]; then
        return 1
    fi
    
    build_farmer
    if [ $? -ne 0 ]; then
        return 1
    fi
    
    print_success "All nodes built successfully!"
    echo ""
    print_status "Available binaries:"
    echo "  - Master node: bin/master-node"
    echo "  - Farmer node: bin/farmer-node"
}

# Main script
main() {
    # Create bin directory if it doesn't exist
    mkdir -p bin

    # Parse command line argument
    local target="${1:-all}"  # Default to 'all' if no argument provided

    case "${target}" in
        master)
            build_master
            ;;
        farmer)
            build_farmer
            ;;
        all)
            build_all
            ;;
        help|--help|-h)
            usage
            exit 0
            ;;
        *)
            print_error "Unknown target: $target"
            usage
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"