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
	"strings"

	"github.com/joshsymonds/savecraft.gg/plugins/gvas"
)

// isGameSave returns true if the filename looks like an actual game save
// (EXPEDITION_*.sav), not a UE config file (EnhancedInputUserSettings.sav, etc.).
func isGameSave(fileName string) bool {
	return strings.HasPrefix(fileName, "EXPEDITION_")
}

func main() {
	enc := json.NewEncoder(os.Stdout)

	// argv[1] is the filename, passed by the daemon.
	fileName := ""
	if len(os.Args) > 1 {
		fileName = os.Args[1]
	}

	// Skip non-save files (UE config files like EnhancedInputUserSettings.sav).
	if fileName != "" && !isGameSave(fileName) {
		writeError(enc, "not_a_save", fmt.Sprintf("skipping non-save file: %s", fileName))
		os.Exit(1)
	}

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
