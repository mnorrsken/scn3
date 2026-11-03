package platform

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cloudProviders maps the start of a CloudStorage folder name to a display name.
var cloudProviders = map[string]string{
	"OneDrive":    "OneDrive",
	"GoogleDrive": "Google Drive",
	"Dropbox":     "Dropbox",
	"Box":         "Box",
}

// CloudFolders lists the sync folders of cloud storage apps. On macOS they all
// live in ~/Library/CloudStorage (File Provider), apart from iCloud Drive.
func CloudFolders() []Volume {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []Volume
	icloud := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
	if fi, err := os.Stat(icloud); err == nil && fi.IsDir() {
		out = append(out, Volume{Path: icloud, Name: "iCloud Drive", Kind: KindCloud, Folder: true})
	}
	dir := filepath.Join(home, "Library", "CloudStorage")
	entries, _ := os.ReadDir(dir)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, Volume{Path: filepath.Join(dir, e.Name()), Name: cloudName(e.Name()), Kind: KindCloud, Folder: true})
	}
	return out
}

// cloudName turns "GoogleDrive-me@example.com" into "Google Drive (me@example.com)".
func cloudName(folder string) string {
	provider, account, _ := strings.Cut(folder, "-")
	name, ok := cloudProviders[provider]
	if !ok {
		name = provider
	}
	if account != "" {
		return name + " (" + account + ")"
	}
	return name
}
