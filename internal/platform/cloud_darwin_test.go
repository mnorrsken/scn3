package platform

import "testing"

func TestCloudName(t *testing.T) {
	for in, want := range map[string]string{
		"GoogleDrive-me@example.com": "Google Drive (me@example.com)",
		"OneDrive-Personal":          "OneDrive (Personal)",
		"Dropbox":                    "Dropbox",
		"pCloud-x":                   "pCloud (x)",
	} {
		if got := cloudName(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
