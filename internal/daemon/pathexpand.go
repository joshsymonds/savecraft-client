package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// knownFolderFallback returns the %USERPROFILE%-relative subfolder for a Known
// Folder pseudo-variable, or "" if the name is not a Known Folder.
func knownFolderFallback(name string) string {
	switch name {
	case "DOCUMENTS":
		return "Documents"
	case "SAVED_GAMES":
		return "Saved Games"
	case "LOCALAPPDATA":
		return "AppData/Local"
	case "LOCALAPPDATA_LOW":
		return "AppData/Local/Low"
	default:
		return ""
	}
}

// expandKnownFolder resolves a Known Folder variable and its fallback,
// returning candidate paths with the remainder appended. Returns nil if
// the variable is not a Known Folder or both resolution and fallback fail.
func expandKnownFolder(varName, remainder string) []string {
	fallbackSuffix := knownFolderFallback(varName)
	if fallbackSuffix == "" {
		return nil
	}

	var candidates []string

	// Primary: resolve via platform Known Folder API.
	if resolved, err := resolveKnownFolder(varName); err == nil && resolved != "" {
		candidates = append(candidates, resolved+remainder)
	}

	// Fallback: %USERPROFILE%/<subfolder> expanded via env var.
	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
		fallback := filepath.Join(userProfile, fallbackSuffix) + remainder
		if len(candidates) == 0 || candidates[0] != fallback {
			candidates = append(candidates, fallback)
		}
	}

	return candidates
}

// expandPaths expands a path template and returns one or more candidate paths.
// Known Folder pseudo-variables (%DOCUMENTS%, %SAVED_GAMES%, %LOCALAPPDATA%,
// %LOCALAPPDATA_LOW%) produce two candidates: the platform-resolved path first,
// then the %USERPROFILE%-based fallback. Duplicates are removed.
// Regular %VAR% templates and ~ produce a single candidate.
func expandPaths(template string) []string {
	if template == "" {
		return []string{""}
	}

	// Extract the first %VAR% token if present.
	start := strings.IndexByte(template, '%')
	if start != -1 {
		if end := strings.IndexByte(template[start+1:], '%'); end != -1 {
			end += start + 1
			varName := template[start+1 : end]
			if candidates := expandKnownFolder(varName, template[end+1:]); len(candidates) > 0 {
				return candidates
			}
		}
	}

	return []string{expandPath(template)}
}

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

// resolveFirstValid tries each candidate from expandPaths and returns the first
// path where at least one resolved directory exists. Falls back to the first
// candidate for error reporting if none exist.
func (d *Daemon) resolveFirstValid(template string, excludeDirs []string) string {
	candidates := expandPaths(template)
	for _, expanded := range candidates {
		dirs := resolveGlob(d.fs, expanded, excludeDirs)
		for _, dir := range dirs {
			info, err := d.fs.Stat(dir)
			if err == nil && info.IsDir() {
				return expanded
			}
		}
	}
	// No candidate had a valid directory — return the first for error reporting.
	return candidates[0]
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
// Directories whose names match any entry in excludeDirs (case-insensitive)
// are skipped. Only directories are included in the result. If the glob
// matches nothing, the original pattern is returned so callers can report
// the path in errors.
func resolveGlob(fsys FS, pattern string, excludeDirs []string) []string {
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
		if isExcludedDir(entry.Name(), excludeDirs) {
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

// isExcludedDir checks if a directory name matches any entry in the
// exclude list (case-insensitive).
func isExcludedDir(name string, excludeDirs []string) bool {
	for _, excluded := range excludeDirs {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}

// isExcludedSave checks if a save file name matches any entry in the
// exclude list (case-insensitive).
func isExcludedSave(name string, excludeSaves []string) bool {
	for _, excluded := range excludeSaves {
		if strings.EqualFold(name, excluded) {
			return true
		}
	}
	return false
}
