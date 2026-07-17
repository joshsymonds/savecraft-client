// Echo plugin: reads stdin, reflects content back as structured GameState.
// Used as the reference implementation for testing the ndjson plugin contract.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm .
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const headerBodyParts = 2

func main() {
	enc := json.NewEncoder(os.Stdout)

	data, readErr := io.ReadAll(os.Stdin)
	if readErr != nil {
		writeError(enc, "parse_error", "failed to read stdin: "+readErr.Error())
		os.Exit(1)
	}

	if statusErr := enc.Encode(map[string]any{
		"type":    "status",
		"message": fmt.Sprintf("Read %d bytes", len(data)),
	}); statusErr != nil {
		os.Exit(1)
	}

	content := string(data)
	lines := strings.SplitN(content, "\n", headerBodyParts)
	name := strings.TrimSpace(lines[0])
	if name == "" {
		name = "unnamed"
	}

	if resultErr := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": name,
			"gameId":   "echo",
		},
		"summary": name,
		"sections": map[string]any{
			"content": map[string]any{
				"description": "Raw file content",
				"data": map[string]any{
					"text":      content,
					"lineCount": len(strings.Split(content, "\n")),
					"byteCount": len(data),
				},
			},
		},
	}); resultErr != nil {
		os.Exit(1)
	}
}

func writeError(enc *json.Encoder, errType, message string) {
	if encodeErr := enc.Encode(map[string]any{
		"type":      "error",
		"errorType": errType,
		"message":   message,
	}); encodeErr != nil {
		os.Exit(1)
	}
}
