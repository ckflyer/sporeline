package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests run with `go test` from the project folder. Each one works
// in its own throwaway folder, so none of them can touch your real log.

func freshStore(t *testing.T) {
	t.Helper()
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store = s
	if tmpl == nil {
		if err := loadTemplates(); err != nil {
			t.Fatal(err)
		}
	}
}

func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /add", handleAddForm)
	mux.HandleFunc("POST /add", handleAdd)
	mux.HandleFunc("POST /component/{id}/delete", handleDelete)
	return guard(mux)
}

func post(h http.Handler, path string, form url.Values, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://localhost:8099"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Importing a mycolog folder keeps every ID exactly as it was, keeps the
// family links, and brings the pictures along.
func TestImportKeepsOriginalIDs(t *testing.T) {
	freshStore(t)
	src := filepath.Join("testdata", "mycolog")

	want, _, _, err := loadLegacy(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 300 {
		t.Fatalf("read %d cultures from the sample, expected 300", len(want))
	}
	rep, err := ImportLegacy(src)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Cultures != 300 || rep.Pictures != 1 {
		t.Fatalf("imported %d cultures and %d pictures", rep.Cultures, rep.Pictures)
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
		if sporelineStyle.MatchString(got.ID) {
			t.Fatalf("%s was reissued a Sporeline-style ID", got.ID)
		}
	}
	for _, c := range store.All() {
		for _, p := range c.Parents {
			if store.Get(p) == nil {
				t.Fatalf("%s points at missing parent %s", c.ID, p)
			}
		}
	}
	// Importing the same folder again adds nothing.
	rep, err = ImportLegacy(src)
	if err != nil || rep.Cultures != 0 || rep.Skipped != 300 {
		t.Fatalf("second import: %+v %v", rep, err)
	}
	// And the saved file on disk has everything, not just the part in memory.
	again, err := OpenStore(store.Dir())
	if err != nil || len(again.data.Components) != 300 {
		t.Fatalf("after reopening: %d cultures, %v", len(again.data.Components), err)
	}
}

// The Add page shows an ID before saving. If the page sends one with the
// wrong generation digit, the server must not keep it.
func TestAddUsesRightGeneration(t *testing.T) {
	freshStore(t)
	parent := &Component{Kind: "spores", Species: "Test"}
	if err := store.Add(parent); err != nil {
		t.Fatal(err)
	}
	h := newMux()

	// Wrong: generation 0 while the parent makes it generation 1.
	w := post(h, "/add", url.Values{"kind": {"myc"}, "parent": {parent.ID}, "id": {"MY0ABC"}}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("add failed: %d %s", w.Code, w.Body)
	}
	if store.Get("MY0ABC") != nil {
		t.Fatal("kept an ID with the wrong generation")
	}
	var made *Component
	for _, c := range store.All() {
		if c.Kind == "myc" {
			made = c
		}
	}
	if made == nil || !strings.HasPrefix(made.ID, "MY1") || made.Gen != 1 {
		t.Fatalf("got %+v", made)
	}

	// Right: the ID the page showed is kept.
	post(h, "/add", url.Values{"kind": {"myc"}, "parent": {parent.ID}, "id": {"MY1XYZ"}}, nil)
	if store.Get("MY1XYZ") == nil {
		t.Fatal("a correct ID from the page was not kept")
	}
}

// The Add page hands the generation of every possible parent to the
// picker, so the preview can update when parents change.
func TestAddPageCarriesGenerations(t *testing.T) {
	freshStore(t)
	p := &Component{Kind: "spores", Species: "Test"}
	store.Add(p)
	r := httptest.NewRequest("GET", "http://localhost:8099/add?kind=myc", nil)
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `gen: 0 ,`) || !strings.Contains(body, `KINDPREFIX = "MY"`) {
		t.Fatal("add page is missing the generation data the preview needs")
	}
	if strings.Contains(body, `<label class="f"><span>Parents`) {
		t.Fatal("parent chooser is inside a label again; clicking its text would remove a parent")
	}
}

// Other websites must not be able to change anything.
func TestGuardBlocksOtherSites(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "spores", Species: "Keep me"}
	store.Add(c)
	h := newMux()
	path := "/component/" + c.ID + "/delete"

	for name, hdr := range map[string]map[string]string{
		"cross-site":   {"Sec-Fetch-Site": "cross-site"},
		"same-site":    {"Sec-Fetch-Site": "same-site"},
		"other origin": {"Origin": "https://evil.example"},
		"other port":   {"Origin": "http://localhost:3000"},
	} {
		if w := post(h, path, nil, hdr); w.Code != http.StatusForbidden {
			t.Fatalf("%s: got %d, expected it to be refused", name, w.Code)
		}
		if store.Get(c.ID) == nil {
			t.Fatalf("%s: entry was deleted", name)
		}
	}

	// A website that points its own name at this computer.
	r := httptest.NewRequest("GET", "http://evil.example:8099/add", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign host name answered with %d", w.Code)
	}

	// Sporeline's own page still works.
	w = post(h, path, nil, map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8099"})
	if w.Code != http.StatusSeeOther || store.Get(c.ID) != nil {
		t.Fatalf("own delete did not go through: %d", w.Code)
	}
}

// Deleting an entry and then restoring a backup brings its pictures back.
func TestRestoreBringsPicturesBack(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "grow", Species: "Pictured"}
	store.Add(c)
	pic := c.ID + "_1.png"
	os.WriteFile(filepath.Join(store.PicsDir(), pic), []byte("picture"), 0o600)
	store.Update(func() { c.Pics = []string{pic} })

	name, err := store.BackupNow("test")
	if err != nil {
		t.Fatal(err)
	}
	store.Delete(c.ID)
	if _, err := os.Stat(filepath.Join(store.PicsDir(), pic)); err == nil {
		t.Fatal("picture still in pics after delete")
	}
	// Tidying up must not erase it: the backup still wants it.
	store.CleanRemovedPics()

	if err := store.RestoreBackup(name); err != nil {
		t.Fatal(err)
	}
	if store.Get(c.ID) == nil {
		t.Fatal("entry did not come back")
	}
	raw, err := os.ReadFile(filepath.Join(store.PicsDir(), pic))
	if err != nil || string(raw) != "picture" {
		t.Fatal("picture did not come back")
	}
}

// A removed picture that no backup mentions is erased for good.
func TestUnneededRemovedPicturesAreErased(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "grow", Species: "Gone for good"}
	store.Add(c)
	pic := c.ID + "_1.png"
	os.WriteFile(filepath.Join(store.PicsDir(), pic), []byte("x"), 0o600)
	store.Update(func() { c.Pics = []string{pic} })
	store.Delete(c.ID) // no backup was ever taken
	store.CleanRemovedPics()
	if _, err := os.Stat(filepath.Join(store.RemovedPicsDir(), pic)); err == nil {
		t.Fatal("picture nobody can restore was kept")
	}
}
