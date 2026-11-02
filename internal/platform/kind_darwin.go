//go:build cgo

package platform

/*
#cgo LDFLAGS: -framework DiskArbitration -framework CoreFoundation
#include <DiskArbitration/DiskArbitration.h>
#include <stdlib.h>
#include <string.h>

enum { kindUnknown, kindInternal, kindExternal, kindNetwork, kindOptical, kindImage };

static int boolKey(CFDictionaryRef d, CFStringRef key) {
	CFTypeRef v = CFDictionaryGetValue(d, key);
	return v != NULL && CFGetTypeID(v) == CFBooleanGetTypeID() && CFBooleanGetValue((CFBooleanRef)v);
}

static int stringKeyIs(CFDictionaryRef d, CFStringRef key, const char *want) {
	CFTypeRef v = CFDictionaryGetValue(d, key);
	if (v == NULL || CFGetTypeID(v) != CFStringGetTypeID()) return 0;
	char buf[128];
	if (!CFStringGetCString((CFStringRef)v, buf, sizeof buf, kCFStringEncodingUTF8)) return 0;
	return strstr(buf, want) != NULL;
}

static int diskKind(const char *path) {
	int kind = kindUnknown;
	DASessionRef session = DASessionCreate(kCFAllocatorDefault);
	if (session == NULL) return kind;
	CFURLRef url = CFURLCreateFromFileSystemRepresentation(kCFAllocatorDefault, (const UInt8 *)path, strlen(path), true);
	DADiskRef disk = url ? DADiskCreateFromVolumePath(kCFAllocatorDefault, session, url) : NULL;
	CFDictionaryRef d = disk ? DADiskCopyDescription(disk) : NULL;
	if (d != NULL) {
		if (boolKey(d, kDADiskDescriptionVolumeNetworkKey)) kind = kindNetwork;
		else if (stringKeyIs(d, kDADiskDescriptionDeviceModelKey, "Disk Image") ||
		         stringKeyIs(d, kDADiskDescriptionDeviceProtocolKey, "Virtual")) kind = kindImage;
		else if (stringKeyIs(d, kDADiskDescriptionMediaKindKey, "CDMedia") ||
		         stringKeyIs(d, kDADiskDescriptionMediaKindKey, "DVDMedia") ||
		         stringKeyIs(d, kDADiskDescriptionMediaKindKey, "BDMedia")) kind = kindOptical;
		else if (!boolKey(d, kDADiskDescriptionDeviceInternalKey) ||
		         boolKey(d, kDADiskDescriptionMediaRemovableKey)) kind = kindExternal;
		else kind = kindInternal;
		CFRelease(d);
	}
	if (disk) CFRelease(disk);
	if (url) CFRelease(url);
	CFRelease(session);
	return kind;
}
*/
import "C"

import "unsafe"

// diskKind asks Disk Arbitration what device holds the volume at path.
func diskKind(path string) string {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	switch C.diskKind(cp) {
	case C.kindExternal:
		return KindExternal
	case C.kindNetwork:
		return KindNetwork
	case C.kindOptical:
		return KindOptical
	case C.kindImage:
		return KindImage
	}
	return KindInternal
}
