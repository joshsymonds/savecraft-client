package daemon

import (
	"os"
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
