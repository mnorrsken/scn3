//go:build !windows

package platform

import "errors"

// ShortcutTarget reads Windows shortcuts, which only exist on Windows.
func ShortcutTarget(string) (string, error) {
	return "", errors.New("shortcuts are only read on Windows")
}
