package platform

// Windows file attributes and reparse tags. They live in a file without a
// build tag so the rule below is tested on every OS.
const (
	winAttrDirectory    = 0x10
	winAttrReparsePoint = 0x400
	winAttrRecallOnOpen = 0x40000 // a cloud folder whose listing is only online

	// Set in the tag of a reparse point that stands for another place:
	// junctions, symbolic links, mounted volumes.
	winReparseTagNameSurrogate = 0x20000000
)

// enterWindowsDir decides whether a scan goes into a listed item, from its
// attributes and, for a reparse point, its tag.
//
// A reparse point that points somewhere else is not entered: a junction or
// symbolic link would count that place twice, or loop. Other reparse points
// are real local folders and are entered; OneDrive folders with Files
// On-Demand are the common case. A cloud folder whose contents exist only
// online is skipped: it holds nothing on this disk, and listing it would make
// the sync app fetch it from the internet.
func enterWindowsDir(attrs, tag uint32) bool {
	switch {
	case attrs&winAttrDirectory == 0:
		return false
	case attrs&winAttrReparsePoint == 0:
		return true
	case tag&winReparseTagNameSurrogate != 0:
		return false
	case attrs&winAttrRecallOnOpen != 0:
		return false
	}
	return true
}
