//go:build cgo

package platform

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>

// Returns NULL on success, or an error text the caller must free.
static char *trashItem(const char *path) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		NSError *err = nil;
		if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:nil error:&err]) {
			return NULL;
		}
		return strdup(err.localizedDescription.UTF8String);
	}
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// Trash moves path to the Trash.
func Trash(path string) error {
	cp := C.CString(path)
	defer C.free(unsafe.Pointer(cp))
	if msg := C.trashItem(cp); msg != nil {
		defer C.free(unsafe.Pointer(msg))
		return fmt.Errorf("trash %s: %w", path, errors.New(C.GoString(msg)))
	}
	return nil
}
