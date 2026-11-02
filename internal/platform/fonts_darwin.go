//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CoreText
#include <CoreFoundation/CoreFoundation.h>
#include <CoreText/CoreText.h>
#include <stdlib.h>

static char *convertgo_copy_string(CFStringRef value) {
	if (value == NULL) return NULL;
	CFIndex size = CFStringGetMaximumSizeForEncoding(CFStringGetLength(value), kCFStringEncodingUTF8) + 1;
	char *text = malloc((size_t)size);
	if (text == NULL) return NULL;
	if (!CFStringGetCString(value, text, size, kCFStringEncodingUTF8)) {
		free(text);
		return NULL;
	}
	return text;
}

static char *convertgo_font_families(void) {
	CFArrayRef families = CTFontManagerCopyAvailableFontFamilyNames();
	if (families == NULL) return NULL;
	CFStringRef joined = CFStringCreateByCombiningStrings(kCFAllocatorDefault, families, CFSTR("\n"));
	char *text = convertgo_copy_string(joined);
	if (joined != NULL) CFRelease(joined);
	CFRelease(families);
	return text;
}

static char *convertgo_current_locale(void) {
	CFLocaleRef locale = CFLocaleCopyCurrent();
	if (locale == NULL) return NULL;
	char *text = convertgo_copy_string(CFLocaleGetIdentifier(locale));
	CFRelease(locale);
	return text;
}
*/
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

func SystemLocale() string {
	value := C.convertgo_current_locale()
	if value == nil {
		return environmentLocale()
	}
	defer C.free(unsafe.Pointer(value))
	return C.GoString(value)
}

func ListFonts() ([]string, error) {
	value := C.convertgo_font_families()
	if value == nil {
		return fontCatalog(nil, errors.New("CoreText could not enumerate installed fonts"))
	}
	defer C.free(unsafe.Pointer(value))
	return fontCatalog(strings.Split(C.GoString(value), "\n"), nil)
}
