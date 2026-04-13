// Package main implements the MTGA Player.log parser plugin.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// LogEntry represents a parsed entry from Player.log.
type LogEntry struct {
	// Arrow is "==>" for outbound requests, "<==" for inbound responses.
	// Empty for event-style entries (e.g., GreToClientEvent).
	Arrow string

	// Label identifies the event type (e.g., "RankGetCombinedRankInfo",
	// "GreToClientEvent", "BotDraftDraftStatus").
	Label string

	// PlayerID is the player's ID from event-style entries.
	PlayerID string

	// JSON is the parsed JSON payload.
	JSON json.RawMessage
}

// Log format patterns from the MTGA Unity logger.
var (
	arrowPattern        = regexp.MustCompile(`^\[UnityCrossThreadLogger\](==>|<==) (\S+)`)
	arrowPatternNew     = regexp.MustCompile(`^(==>|<==) (\S+?)(?:\(|$)`)
	eventPattern        = regexp.MustCompile(`^\[UnityCrossThreadLogger\].+?: (?:Match to )?(\w+)(?: to Match)?: (.+)$`)
	labelPattern        = regexp.MustCompile(`^\[UnityCrossThreadLogger\](\S+)\s*$`)
	detailedLogsPattern = regexp.MustCompile(`DETAILED LOGS: (.*)`)
)

// DecodeLog parses Player.log from a reader using streaming I/O.
// Avoids loading the entire file into memory — processes line-by-line.
func DecodeLog(r io.Reader) []LogEntry {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 10MB max line

	var entries []LogEntry
	var pending *LogEntry // entry waiting for JSON payload
	var jsonBuf bytes.Buffer
	jsonDepth := 0
	inString := false
	escaped := false

	emit := func() {
		if pending != nil {
			if jsonBuf.Len() > 0 {
				pending.JSON = json.RawMessage(bytes.Clone(jsonBuf.Bytes()))
			}
			entries = append(entries, *pending)
			pending = nil
		}
		jsonBuf.Reset()
		jsonDepth = 0
		inString = false
		escaped = false
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")

		// If we're inside a multi-line JSON block, keep collecting.
		if jsonDepth > 0 {
			jsonBuf.WriteByte('\n')
			jsonBuf.WriteString(line)
			jsonDepth, inString, escaped = updateBraceDepth(line, jsonDepth, inString, escaped)
			if jsonDepth == 0 {
				emit()
			}
			continue
		}

		// If we have a pending entry and this line starts JSON, begin collecting.
		if pending != nil {
			trimmed := strings.TrimSpace(line)
			if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
				jsonBuf.WriteString(line)
				jsonDepth, inString, escaped = updateBraceDepth(line, 0, false, false)
				if jsonDepth == 0 {
					// Single-line JSON.
					emit()
				}
				continue
			}
			// Not JSON — emit the pending entry without payload.
			emit()
			// Fall through to try matching this line as a new entry.
		}

		// Try to match a log entry header.
		entry, ok := matchHeader(line)
		if !ok {
			continue
		}
		// If the entry already has inline JSON, emit it directly.
		if entry.JSON != nil {
			entries = append(entries, entry)
			continue
		}
		pending = &entry
	}

	// Flush any final pending entry.
	emit()

	return entries
}

// DecodeLogString is a convenience wrapper for testing.
func DecodeLogString(data string) []LogEntry {
	return DecodeLog(strings.NewReader(data))
}

func matchHeader(line string) (LogEntry, bool) {
	if loc := arrowPattern.FindStringSubmatchIndex(line); loc != nil {
		entry := LogEntry{Arrow: line[loc[2]:loc[3]], Label: line[loc[4]:loc[5]]}
		// Check for inline JSON after the label on the same line.
		rest := strings.TrimSpace(line[loc[1]:])
		if len(rest) > 0 && rest[0] == '{' {
			entry.JSON = json.RawMessage(rest)
		}
		return entry, true
	}
	if m := arrowPatternNew.FindStringSubmatch(line); m != nil {
		return LogEntry{Arrow: m[1], Label: m[2]}, true
	}
	if m := eventPattern.FindStringSubmatch(line); m != nil {
		return LogEntry{PlayerID: m[1], Label: strings.TrimSpace(m[2])}, true
	}
	if m := labelPattern.FindStringSubmatch(line); m != nil {
		if detailedLogsPattern.MatchString(line) {
			return LogEntry{}, false
		}
		return LogEntry{Label: m[1]}, true
	}
	return LogEntry{}, false
}

// updateBraceDepth tracks JSON brace nesting to detect complete objects.
func updateBraceDepth(line string, depth int, inStr, esc bool) (int, bool, bool) {
	for _, ch := range line {
		if esc {
			esc = false
			continue
		}
		if ch == '\\' && inStr {
			esc = true
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch ch {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return depth, inStr, esc
}
