// Satisfactory plugin: parses .sav save files into structured GameState.
// Currently parses the uncompressed header; body sections (machines, power,
// progression, ...) are added incrementally.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

func main() {
	enc := json.NewEncoder(os.Stdout)

	header, err := sav.ParseHeader(os.Stdin)
	if err != nil {
		writeError(enc, errorType(err), err.Error())
		os.Exit(1)
	}

	writeStatusf(enc, "Session %q, build %d, %.1f hours played",
		header.SessionName, header.BuildVersion, header.PlayDuration.Hours())

	if encodeErr := enc.Encode(buildResult(header)); encodeErr != nil {
		os.Exit(1)
	}
}

// errorType maps a parse error to the plugin contract's errorType values.
func errorType(err error) string {
	if uve, _ := errors.AsType[*sav.UnsupportedVersionError](err); uve != nil {
		return "unsupported_version"
	}
	return "corrupt_file"
}

// buildResult assembles the ndjson result line. Identity is keyed by session
// name, not file name: Satisfactory rotates autosaves across three files
// (Session_autosave_0/1/2), and all of them are the same logical save.
func buildResult(h *sav.Header) map[string]any {
	return map[string]any{
		"type": "result",
		"identity": map[string]any{
			"saveName": h.SessionName,
			"gameId":   "satisfactory",
		},
		"summary":  buildSummary(h),
		"sections": buildSections(h),
	}
}

func buildSummary(h *sav.Header) string {
	summary := fmt.Sprintf("%s, %.1f hours played", h.SessionName, h.PlayDuration.Hours())
	if h.CreativeMode {
		summary += " (creative)"
	}
	if h.Modded {
		summary += " (modded)"
	}
	return summary
}

func buildSections(h *sav.Header) map[string]any {
	return map[string]any{
		"game_overview": map[string]any{
			"description": "Save metadata: session name, playtime, game build, save timestamp, creative/modded flags — fetch first to orient on which factory world this is",
			"data": map[string]any{
				"sessionName":     h.SessionName,
				"saveName":        h.SaveName,
				"mapName":         h.MapName,
				"playTimeSeconds": int32(h.PlayDuration.Seconds()),
				"playTimeHours":   fmt.Sprintf("%.1f", h.PlayDuration.Hours()),
				"savedAt":         h.SaveTime.Format("2006-01-02T15:04:05Z"),
				"gameBuild":       h.BuildVersion,
				"saveVersion":     h.SaveVersion,
				"creativeMode":    h.CreativeMode,
				"modded":          h.Modded,
			},
		},
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
