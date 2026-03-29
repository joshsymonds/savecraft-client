// Factorio plugin: pass-through parser for JSON state exported by the Savecraft Lua mod.
// The mod writes structured JSON to script-output/savecraft/state.json; this parser
// validates the structure and converts it to Savecraft's ndjson contract.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// modExport is the JSON structure written by the Factorio Lua mod.
type modExport struct {
	Identity identity                   `json:"identity"`
	Summary  string                     `json:"summary"`
	Sections map[string]json.RawMessage `json:"sections"`
}

type identity struct {
	SaveName string `json:"save_name"`
	GameID   string `json:"game_id"`
}

// section is the expected structure of each section value.
type section struct {
	Description string          `json:"description"`
	Data        json.RawMessage `json:"data"`
}

func main() {
	enc := json.NewEncoder(os.Stdout)

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(enc, "read_error", "failed to read stdin: "+err.Error())
		os.Exit(1)
	}

	writeStatus(enc, "Parsing Factorio state export...")

	var export modExport
	if err := json.Unmarshal(data, &export); err != nil {
		writeError(enc, "corrupt_file", "invalid JSON: "+err.Error())
		os.Exit(1)
	}

	if export.Identity.SaveName == "" {
		writeError(enc, "corrupt_file", "missing identity.save_name")
		os.Exit(1)
	}
	if export.Identity.GameID == "" {
		export.Identity.GameID = "factorio"
	}
	if len(export.Sections) == 0 {
		writeError(enc, "corrupt_file", "no sections in export")
		os.Exit(1)
	}

	// Validate each section: must have description and data as a JSON object.
	sections := make(map[string]any, len(export.Sections))
	for name, raw := range export.Sections {
		var sec section
		if err := json.Unmarshal(raw, &sec); err != nil {
			writeError(enc, "corrupt_file", fmt.Sprintf("section %q: %v", name, err))
			os.Exit(1)
		}
		if len(sec.Data) == 0 {
			writeError(enc, "corrupt_file", fmt.Sprintf("section %q: missing data", name))
			os.Exit(1)
		}
		// Verify data is a JSON object (starts with '{'), not an array or scalar.
		firstByte := firstNonWhitespace(sec.Data)
		if firstByte != '{' {
			writeError(enc, "corrupt_file", fmt.Sprintf("section %q: data must be a JSON object, got %q", name, string(firstByte)))
			os.Exit(1)
		}
		sections[name] = map[string]any{
			"description": sec.Description,
			"data":        json.RawMessage(sec.Data),
		}
	}

	writeStatus(enc, fmt.Sprintf("Validated %d sections", len(sections)))

	if err := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": export.Identity.SaveName,
			"gameId":   export.Identity.GameID,
		},
		"summary":  export.Summary,
		"sections": sections,
	}); err != nil {
		os.Exit(1)
	}
}

func firstNonWhitespace(data []byte) byte {
	for _, b := range data {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return b
		}
	}
	return 0
}

func writeStatus(enc *json.Encoder, message string) {
	if err := enc.Encode(map[string]any{
		"type":    "status",
		"message": message,
	}); err != nil {
		os.Exit(1)
	}
}

func writeError(enc *json.Encoder, errType, message string) {
	if err := enc.Encode(map[string]any{
		"type":      "error",
		"errorType": errType,
		"message":   message,
	}); err != nil {
		os.Exit(1)
	}
}
