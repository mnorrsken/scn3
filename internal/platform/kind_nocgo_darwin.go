//go:build !cgo

package platform

// diskKind needs Disk Arbitration, which needs cgo. The CLI does not show kinds.
func diskKind(string) string { return KindInternal }
