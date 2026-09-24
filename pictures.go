package main

import (
	"encoding/base64"
	"encoding/json"
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

// ---------- removed pictures ----------
//
// Deleting an entry or a picture does not erase the file straight away.
// It moves to a "removed-pics" folder instead, so that restoring an older
// backup brings the pictures back along with the entries. A removed
// picture is only erased for good once no backup mentions it any more,
// which with the default settings is about a month later.

func (s *Store) RemovedPicsDir() string { return filepath.Join(s.dir, "removed-pics") }

// retirePic moves one picture out of the way instead of erasing it.
func (s *Store) retirePic(name string) {
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		return
	}
	from := filepath.Join(s.PicsDir(), name)
	if _, err := os.Stat(from); err != nil {
		return
	}
	os.MkdirAll(s.RemovedPicsDir(), 0o755)
	to := filepath.Join(s.RemovedPicsDir(), name)
	if err := os.Rename(from, to); err != nil {
		// A rename can fail across drives; copy then remove instead.
		if raw, err := os.ReadFile(from); err == nil {
			if os.WriteFile(to, raw, 0o600) == nil {
				os.Remove(from)
			}
		}
	}
}

// bringBackPics moves any picture that d refers to, and that is sitting
// in removed-pics, back into pics. Returns how many came back.
func (s *Store) bringBackPics(d Data) int {
	n := 0
	for _, c := range d.Components {
		for _, p := range c.Pics {
			name := filepath.Base(p)
			if name == "" || name == "." || name == ".." {
				continue
			}
			live := filepath.Join(s.PicsDir(), name)
			if _, err := os.Stat(live); err == nil {
				continue
			}
			if os.Rename(filepath.Join(s.RemovedPicsDir(), name), live) == nil {
				n++
			}
		}
	}
	return n
}

// CleanRemovedPics erases removed pictures that neither the log nor any
// remaining backup refers to. Nothing could ever bring those back.
func (s *Store) CleanRemovedPics() {
	entries, err := os.ReadDir(s.RemovedPicsDir())
	if err != nil || len(entries) == 0 {
		return
	}
	wanted := map[string]bool{}
	note := func(d Data) {
		for _, c := range d.Components {
			for _, p := range c.Pics {
				wanted[filepath.Base(p)] = true
			}
		}
	}
	note(s.Snapshot())
	for _, b := range s.Backups() {
		raw, err := os.ReadFile(filepath.Join(s.BackupsDir(), b.Name))
		if err != nil {
			return // cannot be sure, so erase nothing
		}
		var d Data
		if err := json.Unmarshal(raw, &d); err != nil {
			continue // not a backup we can read; it cannot restore pictures either
		}
		note(d)
	}
	for _, e := range entries {
		if !e.IsDir() && !wanted[e.Name()] {
			os.Remove(filepath.Join(s.RemovedPicsDir(), e.Name()))
		}
	}
}
