//go:build js && wasm
// +build js,wasm

package main

import "github.com/ShadSpace/shadspace-go-v2/pkg/wasm"

func main() {
	// This will register functions and keep the program running
	wasm.RegisterFunctions()
}
