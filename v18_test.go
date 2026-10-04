package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tree from the bug report: a parent and its two children all stored
// as generation 1, although the IDs say 3 and 4. Opening the log must
// put them back, and the tree must draw the parent above its children.
func TestGenerationsRepairedOnOpen(t *testing.T) {
	dir := t.TempDir()
	broken := Data{Version: 1, Settings: defaultSettings(), Components: []*Component{
		{ID: "MY3N5D", Kind: "myc", Species: "Lion's Mane", Gen: 1, Created: "2026-09-20"},
		{ID: "MY4KP9", Kind: "myc", Species: "Lion's Mane", Gen: 1, Created: "2026-09-23", Parents: []string{"MY3N5D"}},
		{ID: "MY4F5D", Kind: "myc", Species: "Lion's Mane", Gen: 1, Created: "2026-09-23", Parents: []string{"MY3N5D"}, Gone: true},
		// A mycolog token says nothing about generation; it still has to
		// sit below its parent.
		{ID: "8WB4", Kind: "spawn", Species: "Lion's Mane", Gen: 0, Created: "2026-09-25", Parents: []string{"MY4KP9"}},
	}}
	b, _ := json.Marshal(broken)
	os.WriteFile(filepath.Join(dir, "sporeline.json"), b, 0o600)

	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store = s
	for id, want := range map[string]int{"MY3N5D": 3, "MY4KP9": 4, "MY4F5D": 4, "8WB4": 5} {
		if got := s.Get(id).Gen; got != want {
			t.Errorf("%s: generation %d, want %d", id, got, want)
		}
	}
	// Saved to disk, not just fixed in memory.
	again, _ := OpenStore(dir)
	if again.Get("MY4KP9").Gen != 4 {
		t.Fatal("repair was not saved")
	}
	// Gone with no reason reads as "unknown", which shows as plain "Gone".
	if r := s.Get("MY4F5D").GoneReason; r != "unknown" {
		t.Fatalf("gone reason %q", r)
	}
	svg := s.FamilySVG(s.CloseLineage("MY4KP9"), "MY4KP9")
	if !strings.Contains(svg, "GEN 3") || !strings.Contains(svg, "GEN 4") {
		t.Fatal("tree should have a row for generation 3 and one for 4")
	}
}

// Raising is fine, lowering never: a culture whose ancestors were deleted
// keeps its generation.
func TestDeletingAncestorsKeepsGeneration(t *testing.T) {
	freshStore(t)
	a := &Component{Kind: "spores", Species: "X"}
	store.Add(a)
	b := &Component{Kind: "myc", Species: "X", Parents: []string{a.ID}}
	store.Add(b)
	c := &Component{Kind: "myc", Species: "X", Parents: []string{b.ID}}
	store.Add(c)
	store.Delete(a.ID)
	store.Delete(b.ID)
	if got := store.Get(c.ID).Gen; got != 2 {
		t.Fatalf("generation %d after deleting ancestors, want 2", got)
	}
}

func TestGoneLabels(t *testing.T) {
	for key, want := range map[string]string{"": "", "unknown": "", "nonsense": "", "contaminated": "Contaminated"} {
		if got := GoneReasonLabel(key); got != want {
			t.Errorf("GoneReasonLabel(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestGoneReasonMustBeKnown(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "myc", Species: "X"}
	store.Add(c)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /component/{id}/gone", handleGone)
	post(guard(mux), "/component/"+c.ID+"/gone", url.Values{"reason": {"<script>"}, "goneAt": {"2026-10-01"}}, nil)
	if c.GoneReason != "unknown" || c.GoneAt != "2026-10-01" || !c.Gone {
		t.Fatalf("got %+v", c)
	}
	post(guard(mux), "/component/"+c.ID+"/gone", url.Values{"back": {"1"}}, nil)
	if c.Gone || c.GoneReason != "" {
		t.Fatal("back on hand should clear the gone details")
	}
}

func TestSearchShowsWhatWasTyped(t *testing.T) {
	freshStore(t)
	store.Add(&Component{Kind: "myc", Species: "Lion's Mane"})
	r := httptest.NewRequest("GET", "http://localhost:8099/list/all?q=LiON&f=1", nil)
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /list/{kind}", handleList)
	mux.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, `value="LiON"`) {
		t.Fatal("search box should keep the capitals that were typed")
	}
	if !strings.Contains(body, "Lion&#39;s Mane") {
		t.Fatal("search should still match without caring about case")
	}
}

func TestURLEscapeKeepsEverything(t *testing.T) {
	msg := "Backed up to sporeline-1+2 100% & done?#"
	got, _ := url.QueryUnescape(urlEscape(msg))
	if got != msg {
		t.Fatalf("round trip gave %q", got)
	}
}

// Export with pictures, import on a fresh machine, and the picture is
// there. Export without, and the picture can be put back by name.
func TestExportCarriesPictures(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "grow", Species: "X", Pics: []string{"GR1ABC_1.png"}}
	store.Add(c)
	pic := []byte("\x89PNG pretend")
	os.WriteFile(filepath.Join(store.PicsDir(), "GR1ABC_1.png"), pic, 0o600)
	store.Settings() // settings object sits last in the JSON; must survive
	var with, without bytes.Buffer
	if err := writeBundle(&with, store.Snapshot(), true); err != nil {
		t.Fatal(err)
	}
	writeBundle(&without, store.Snapshot(), false)

	var b bundle
	if err := json.Unmarshal(with.Bytes(), &b); err != nil {
		t.Fatalf("export is not valid JSON: %v\n%s", err, with.String())
	}
	if len(b.Pictures) != 1 || b.Settings.BackupKeep != 30 || len(b.Components) != 1 {
		t.Fatalf("bundle: %d pictures, keep %d", len(b.Pictures), b.Settings.BackupKeep)
	}

	// Import the picture-less file somewhere new.
	freshStore(t)
	var d Data
	json.Unmarshal(without.Bytes(), &d)
	store.Merge(d)
	if n := len(missingPics()); n != 1 {
		t.Fatalf("%d missing, want 1", n)
	}
	// Put it back through the Data page, alongside a stray file.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, data := range map[string][]byte{"GR1ABC_1.png": pic, "holiday.jpg": []byte("x")} {
		fw, _ := mw.CreateFormFile("pic", name)
		fw.Write(data)
	}
	mw.Close()
	r := httptest.NewRequest("POST", "http://localhost:8099/pics/reattach", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	handlePicsReattach(w, r)
	got, err := os.ReadFile(filepath.Join(store.PicsDir(), "GR1ABC_1.png"))
	if err != nil || !bytes.Equal(got, pic) {
		t.Fatal("picture was not put back")
	}
	if _, err := os.Stat(filepath.Join(store.PicsDir(), "holiday.jpg")); err == nil {
		t.Fatal("a file the log does not name should be left out")
	}
	if len(missingPics()) != 0 {
		t.Fatal("still missing after reattach")
	}
}
