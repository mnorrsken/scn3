package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// syncRoots is where Windows keeps the folders of apps that use the Cloud
// Files API: OneDrive, Dropbox, Box, iCloud for Windows and others.
const syncRoots = `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\SyncRootManager`

var syncRootNames = map[string]string{
	"OneDrive":    "OneDrive",
	"iCloudDrive": "iCloud Drive",
	"Dropbox":     "Dropbox",
	"Box":         "Box",
}

// CloudFolders lists the sync folders registered for the current user.
func CloudFolders() []Volume {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil
	}
	sid := user.User.Sid.String()
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, syncRoots, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer root.Close()
	ids, _ := root.ReadSubKeyNames(-1)
	sort.Strings(ids)
	var out []Volume
	for _, id := range ids {
		k, err := registry.OpenKey(root, id+`\UserSyncRoots`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		path, _, err := k.GetStringValue(sid)
		k.Close()
		if err != nil || path == "" {
			continue
		}
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, Volume{Path: filepath.Clean(path), Name: syncRootName(id, path), Kind: KindCloud, Folder: true})
	}
	return out
}

// syncRootName makes a display name from a sync root ID such as
// "OneDrive!S-1-5-21-…!Personal".
func syncRootName(id, _ string) string {
	provider, rest, _ := strings.Cut(id, "!")
	name, ok := syncRootNames[provider]
	if !ok {
		name = provider
	}
	if _, account, ok := strings.Cut(rest, "!"); ok && account != "" {
		account, _, _ = strings.Cut(account, "|")
		return name + " (" + account + ")"
	}
	return name
}

// ShortcutTarget returns where a Windows shortcut (.lnk) points, using the
// Shell's own reader so every kind of shortcut is understood.
func ShortcutTarget(lnk string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		var oleErr *ole.OleError
		// S_FALSE: COM was already set up on this thread, which is fine.
		if !errors.As(err, &oleErr) || oleErr.Code() != 1 {
			return "", err
		}
	}
	defer ole.CoUninitialize()
	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return "", err
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return "", err
	}
	defer shell.Release()
	v, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return "", err
	}
	link := v.ToIDispatch()
	defer link.Release()
	target, err := oleutil.GetProperty(link, "TargetPath")
	if err != nil {
		return "", err
	}
	if t := target.ToString(); t != "" {
		return t, nil
	}
	return "", errors.New("the shortcut has no target path")
}
