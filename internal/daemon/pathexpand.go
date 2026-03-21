package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// expandPath expands ~ and %VAR% templates in a path string.
// ~ is expanded to the user's home directory.
// %VAR% patterns are expanded to the corresponding environment variable.
func expandPath(template string) string {
	if template == "" {
		return template
	}

	// Expand ~ to home directory.
	if template == "~" || strings.HasPrefix(template, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if template == "~" {
				return home
			}
			template = home + template[1:]
		}
	}

	// Expand %VAR% patterns.
	result := template
	for {
		start := strings.IndexByte(result, '%')
		if start == -1 {
			break
		}
		end := strings.IndexByte(result[start+1:], '%')
		if end == -1 {
			break
		}
		end += start + 1
		varName := result[start+1 : end]
		if varName == "" {
			break
		}
		result = result[:start] + os.Getenv(varName) + result[end+1:]
	}

	return result
}

// hasGlobMeta reports whether the path contains any glob metacharacters.
func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

// resolveGlob expands glob patterns in a path to concrete directory paths
// using the provided filesystem interface. If the path contains no glob
// metacharacters, it is returned as-is (wrapped in a single-element slice).
//
// Only the last path segment may contain glob characters. For example,
// "/saves/*" is supported, but "/*/saves" is not (only the final segment
// is expanded). This covers the common case of per-user subdirectories
// like Steam ID folders.
//
// Only directories are included in the result. If the glob matches nothing,
// the original pattern is returned so callers can report the path in errors.
func resolveGlob(fsys FS, pattern string) []string {
	if !hasGlobMeta(pattern) {
		return []string{pattern}
	}

	// Split into parent directory and glob segment.
	parentDir := filepath.Dir(pattern)
	globPart := filepath.Base(pattern)

	// If the parent itself has glob chars, we don't support nested globs.
	// Return the pattern as-is for error reporting.
	if hasGlobMeta(parentDir) {
		return []string{pattern}
	}

	entries, err := fsys.ReadDir(parentDir)
	if err != nil {
		return []string{pattern}
	}

	var matches []string
	for _, entry := range entries {
		matched, matchErr := filepath.Match(globPart, entry.Name())
		if matchErr != nil || !matched {
			continue
		}
		full := filepath.Join(parentDir, entry.Name())
		// Only include directories.
		info, statErr := fsys.Stat(full)
		if statErr != nil || !info.IsDir() {
			continue
		}
		matches = append(matches, full)
	}

	if len(matches) == 0 {
		return []string{pattern}
	}

	sort.Strings(matches)
	return matches
}
