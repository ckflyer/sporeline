package main

import (
	"os"
	"regexp"
	"testing"
)

// A fresh install importing a mycolog folder must keep every ID exactly
// as it was. If this ever fails, new users would land in the repair
// flow, which is precisely what we do not want.
func TestImportKeepsOriginalIDs(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sl")
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store = s

	want, _, _, err := loadLegacy("/home/claude/myco/demo/mycolog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportLegacy("/home/claude/myco/demo/mycolog"); err != nil {
		t.Fatal(err)
	}

	sporelineStyle := regexp.MustCompile(`^(SE|MY|SN|GR)[0-9A-Z]{4}$`)
	for _, lc := range want {
		got := store.Get(lc.Token)
		if got == nil {
			t.Fatalf("ID %s was not kept", lc.Token)
		}
		if got.Species != lc.Species || got.Created != lc.Created {
			t.Fatalf("%s holds the wrong entry", lc.Token)
		}
	}
	for _, c := range store.All() {
		if sporelineStyle.MatchString(c.ID) {
			t.Fatalf("%s was reissued a Sporeline-style ID", c.ID)
		}
	}
	// Parent links point at original IDs too.
	for _, c := range store.All() {
		for _, p := range c.Parents {
			if store.Get(p) == nil {
				t.Fatalf("%s points at missing parent %s", c.ID, p)
			}
		}
	}
	t.Logf("kept all %d original IDs", len(want))
}
