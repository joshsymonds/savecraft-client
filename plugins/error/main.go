// Error plugin: always emits a structured error and exits with code 1.
// Used for testing the error path of the ndjson plugin contract.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm .
package main

import (
	"encoding/json"
	"os"
)

func main() {
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"type":      "error",
		"errorType": "corrupt_file",
		"message":   "test error from error plugin",
	}); err != nil {
		os.Exit(1)
	}
	os.Exit(1)
}
