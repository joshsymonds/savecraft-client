//go:build windows

package daemon

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// resolveKnownFolder returns the real filesystem path for a Windows Known Folder
// using SHGetKnownFolderPath. This resolves correctly even when folders are
// redirected by OneDrive or other mechanisms.
// Supported names: DOCUMENTS, SAVED_GAMES, LOCALAPPDATA, LOCALAPPDATA_LOW.
func resolveKnownFolder(name string) (string, error) {
	var folderID *windows.KNOWNFOLDERID
	switch name {
	case "DOCUMENTS":
		folderID = windows.FOLDERID_Documents
	case "SAVED_GAMES":
		folderID = windows.FOLDERID_SavedGames
	case "LOCALAPPDATA":
		folderID = windows.FOLDERID_LocalAppData
	case "LOCALAPPDATA_LOW":
		folderID = windows.FOLDERID_LocalAppDataLow
	default:
		return "", fmt.Errorf("resolve known folder: unknown folder %q", name)
	}

	path, err := windows.KnownFolderPath(folderID, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("resolve known folder %s: %w", name, err)
	}
	return path, nil
}
