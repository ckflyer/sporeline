package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// writePictures unpacks base64 pictures from an import bundle into the
// pics folder. Names are taken as-is (minus any path), so the filenames
// referenced by components keep working.
func writePictures(pics map[string]string) int {
	n := 0
	for name, b64 := range pics {
		clean := filepath.Base(strings.TrimSpace(name))
		if clean == "" || clean == "." || clean == ".." {
			continue
		}
		if i := strings.Index(b64, ","); i >= 0 && strings.HasPrefix(b64, "data:") {
			b64 = b64[i+1:]
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(store.PicsDir(), clean), raw, 0o600); err != nil {
			continue
		}
		n++
	}
	return n
}
