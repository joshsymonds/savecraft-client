// Satisfactory plugin: parses .sav save files into structured GameState.
// Streams the body in one pass, extracting progression managers and the
// player; more sections (machines, power, storage, ...) are added
// incrementally.
//
// Build: GOOS=wasip1 GOARCH=wasm go build -o parser.wasm ./parser
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/joshsymonds/savecraft.gg/plugins/satisfactory/sav"
)

// stderr is the unstructured debug log sink (captured by the daemon).
func stderr() io.Writer { return os.Stderr }

func main() {
	enc := json.NewEncoder(os.Stdout)

	// Defense in depth: the parser must emit a clean ndjson error instead
	// of crashing on any input, including panics from undiscovered decode
	// bugs. (A true stack overflow is still fatal — recover cannot catch it.)
	defer func() {
		if r := recover(); r != nil {
			writeError(enc, "corrupt_file", fmt.Sprintf("parser panic: %v", r))
			os.Exit(1)
		}
	}()

	header, body, err := sav.Open(os.Stdin)
	if err != nil {
		writeError(enc, errorType(err), err.Error())
		os.Exit(1)
	}

	writeStatusf(enc, "Session %q, build %d, %.1f hours played",
		header.SessionName, header.BuildVersion, header.PlayDuration.Hours())

	state := newSaveState(header)
	if err := sav.Extract(header, body, state.want, state.collect); err != nil {
		writeError(enc, errorType(err), err.Error())
		os.Exit(1)
	}
	state.resolve()

	if encodeErr := enc.Encode(state.buildResult()); encodeErr != nil {
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
