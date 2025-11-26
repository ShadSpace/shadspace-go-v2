//go:build js && wasm
// +build js,wasm

package wasm

import (
	"syscall/js"
)

// Global SDK instance
var sdk *ShadSpaceSDK

func init() {
	sdk = NewShadSpaceSDK()
}

// RegisterFunctions registers Go functions for JS access
func RegisterFunctions() {
	// Create a channel to keep the Go program running
	done := make(chan struct{}, 0)

	// Create the SDK functions object
	sdkFuncs := js.ValueOf(map[string]interface{}{
		"init":            js.FuncOf(initSDK),
		"uploadFile":      js.FuncOf(uploadFile),
		"downloadFile":    js.FuncOf(downloadFile),
		"listFiles":       js.FuncOf(listFiles),
		"getNetworkStats": js.FuncOf(getNetworkStats),
		"getNodeInfo":     js.FuncOf(getNodeInfo),
		"close":           js.FuncOf(closeSDK),
	})

	// Set the global ShadSpaceSDK object
	js.Global().Set("ShadSpaceSDK", sdkFuncs)

	// Log success
	js.Global().Get("console").Call("log", "ShadSpace WASM functions registered successfully")

	// Keep the program running
	<-done
}

func initSDK(this js.Value, args []js.Value) interface{} {
	if len(args) == 0 {
		return createErrorResult("Configuration required")
	}

	configJSON := args[0].String()

	result, err := sdk.Init(configJSON)
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func uploadFile(this js.Value, args []js.Value) interface{} {
	if len(args) < 4 {
		return createErrorResult("Invalid arguments: expected fileData, fileName, totalShards, requiredShards")
	}

	// Get file data from Uint8Array
	var fileData []byte
	if args[0].InstanceOf(js.Global().Get("Uint8Array")) {
		length := args[0].Get("length").Int()
		fileData = make([]byte, length)
		js.CopyBytesToGo(fileData, args[0])
	} else {
		return createErrorResult("Expected Uint8Array for file data")
	}

	fileName := args[1].String()
	totalShards := args[2].Int()
	requiredShards := args[3].Int()

	result, err := sdk.UploadFile(fileData, fileName, totalShards, requiredShards)
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func downloadFile(this js.Value, args []js.Value) interface{} {
	if len(args) == 0 {
		return createErrorResult("File hash required")
	}

	fileHash := args[0].String()
	result, err := sdk.DownloadFile(fileHash)
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func listFiles(this js.Value, args []js.Value) interface{} {
	result, err := sdk.ListFiles()
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func getNetworkStats(this js.Value, args []js.Value) interface{} {
	result, err := sdk.GetNetworkStats()
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func getNodeInfo(this js.Value, args []js.Value) interface{} {
	result, err := sdk.GetNodeInfo()
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult(result)
}

func closeSDK(this js.Value, args []js.Value) interface{} {
	err := sdk.Close()
	if err != nil {
		return createErrorResult(err.Error())
	}

	return createSuccessResult("SDK closed successfully")
}

// Helper functions for creating JS results
func createSuccessResult(data string) map[string]interface{} {
	return map[string]interface{}{
		"success": true,
		"result":  data,
	}
}

func createErrorResult(message string) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"error":   message,
	}
}
