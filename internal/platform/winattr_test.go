package platform

import "testing"

func TestEnterWindowsDir(t *testing.T) {
	const (
		file        = 0x20  // FILE_ATTRIBUTE_ARCHIVE
		dir         = 0x10  // FILE_ATTRIBUTE_DIRECTORY
		reparse     = 0x400 // FILE_ATTRIBUTE_REPARSE_POINT
		pinned      = 0x80000
		recall      = 0x40000
		junction    = 0xA0000003 // IO_REPARSE_TAG_MOUNT_POINT
		symlink     = 0xA000000C // IO_REPARSE_TAG_SYMLINK
		oneDrive    = 0x9000601A // IO_REPARSE_TAG_CLOUD_6
		projFS      = 0x9000001C // IO_REPARSE_TAG_PROJFS
		appExecLink = 0x8000001B // IO_REPARSE_TAG_APPEXECLINK
	)
	for _, c := range []struct {
		name  string
		attrs uint32
		tag   uint32
		want  bool
	}{
		{"plain folder", dir, 0, true},
		{"file", file, 0, false},
		{"junction", dir | reparse, junction, false},
		{"directory symlink", dir | reparse, symlink, false},
		{"OneDrive folder kept on this device", dir | reparse | pinned, oneDrive, true},
		{"OneDrive folder with files on demand", dir | reparse, oneDrive, true},
		{"OneDrive folder only online", dir | reparse | recall, oneDrive, false},
		{"ProjFS folder", dir | reparse, projFS, true},
		{"app execution alias (a file)", file | reparse, appExecLink, false},
	} {
		if got := enterWindowsDir(c.attrs, c.tag); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
