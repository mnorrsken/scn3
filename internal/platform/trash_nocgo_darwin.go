//go:build darwin && !cgo

package platform

import "errors"

// Trash is not available without cgo. The CLI does not need it.
func Trash(path string) error {
	return errors.New("trash needs a cgo build")
}
