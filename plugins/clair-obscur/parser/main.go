// Clair Obscur: Expedition 33 plugin. Parses GVAS .sav save files into
// structured GameState sections for AI assistants.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/joshsymonds/savecraft.gg/plugins/gvas"
)

func main() {
	enc := json.NewEncoder(os.Stdout)

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeError(enc, "read_error", fmt.Sprintf("failed to read stdin: %v", err))
		os.Exit(1)
	}

	save, err := gvas.ParseBytes(data)
	if err != nil {
		writeError(enc, "parse_error", err.Error())
		os.Exit(1)
	}

	writeStatusf(enc, "Clair Obscur save, %d bytes", len(data))

	sections := buildAllSections(save)
	summary := buildSummary(save)
	saveName := buildSaveName(save)

	if err := enc.Encode(map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": saveName,
			"gameId":   "clair-obscur",
		},
		"summary":  summary,
		"sections": sections,
	}); err != nil {
		os.Exit(1)
	}
}

func writeStatusf(enc *json.Encoder, format string, args ...any) {
	if err := enc.Encode(map[string]any{
		"type":    "status",
		"message": fmt.Sprintf(format, args...),
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
