package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A log written by an older version, with one total yield and a free-text
// flushes line, opens with both kept as a flush entry and nothing lost.
func TestOldYieldBecomesFlush(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"components":[
	  {"id":"GR5AAA","kind":"grow","species":"P. cubensis","gen":5,"created":"2026-08-13","yield":129.8,"parents":[]},
	  {"id":"GR5BBB","kind":"grow","species":"P. cubensis","gen":5,"created":"2026-08-13","flushes":"1st 340, 2nd 190","parents":[]},
	  {"id":"GR5CCC","kind":"grow","species":"P. cubensis","gen":5,"created":"2026-08-13","parents":[]}
	],"settings":{"backupKeep":30}}`
	os.WriteFile(filepath.Join(dir, "sporeline.json"), []byte(old), 0o600)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	store = s
	a := s.Get("GR5AAA")
	if len(a.FlushLog) != 1 || a.FlushLog[0].Unmarked != 129.8 || a.Yield != 0 {
		t.Fatalf("total yield not carried over: %+v", a.FlushLog)
	}
	bb := s.Get("GR5BBB")
	if len(bb.FlushLog) != 1 || !strings.Contains(bb.FlushLog[0].Note, "1st 340, 2nd 190") {
		t.Fatalf("flush notes not carried over: %+v", bb.FlushLog)
	}
	if len(s.Get("GR5CCC").FlushLog) != 0 {
		t.Fatal("a grow with no harvest got an empty flush")
	}
	// Saved in the new shape only, and opening again does not double up.
	s.Save()
	raw, _ := os.ReadFile(filepath.Join(dir, "sporeline.json"))
	if strings.Contains(string(raw), `"yield"`) || strings.Contains(string(raw), `"flushes"`) {
		t.Fatal("old fields still written")
	}
	s2, _ := OpenStore(dir)
	if len(s2.Get("GR5AAA").FlushLog) != 1 {
		t.Fatal("opening twice duplicated the flush")
	}
}

// Restoring or importing an old backup converts it the same way.
func TestOldBackupConvertsOnMerge(t *testing.T) {
	freshStore(t)
	var d Data
	json.Unmarshal([]byte(`{"components":[{"id":"GR1XYZ","kind":"grow","species":"x","yield":50,"parents":[]}]}`), &d)
	if _, err := store.Merge(d); err != nil {
		t.Fatal(err)
	}
	if c := store.Get("GR1XYZ"); len(c.FlushLog) != 1 || c.FlushLog[0].Unmarked != 50 {
		t.Fatalf("got %+v", c.FlushLog)
	}
}

func TestParseGrams(t *testing.T) {
	good := map[string]float64{"": 0, "42": 42, "12.5": 12.5, "12,5": 12.5, "30 g": 30, "7grams": 7, " 3.2G ": 3.2}
	for in, want := range good {
		if got, ok := parseGrams(in); !ok || got != want {
			t.Errorf("%q: got %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"lots", "-5", "1e9"} {
		if _, ok := parseGrams(in); ok {
			t.Errorf("%q should be refused", in)
		}
	}
}

// Adding, editing, marking and removing flushes from the culture page.
func TestFlushes(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "grow", Species: "x", Yield: 100}
	store.Add(c) // saved through the upgrade, so the old total is flush 1
	h := newMux()
	path := "/component/" + c.ID + "/flush"

	post(h, path+"/mark", url.Values{"n": {"0"}, "as": {"dry"}}, nil)
	if c.FlushLog[0].Dry != 100 || c.FlushLog[0].Unmarked != 0 {
		t.Fatalf("mark: %+v", c.FlushLog)
	}
	post(h, path, url.Values{"date": {"2026-09-01"}, "wet": {"400"}}, nil)
	post(h, path, url.Values{"wet": {""}, "dry": {""}}, nil) // nothing to add
	if len(c.FlushLog) != 2 {
		t.Fatalf("add: %+v", c.FlushLog)
	}
	post(h, path, url.Values{"n": {"1"}, "date": {"2026-09-01"}, "wet": {"400"}, "dry": {"38,5"}}, nil)
	if c.FlushLog[1].Dry != 38.5 || c.FlushLog[1].Wet != 400 {
		t.Fatalf("edit: %+v", c.FlushLog[1])
	}
	post(h, path, url.Values{"n": {"1"}, "wet": {"lots"}}, nil) // refused, nothing changes
	if c.FlushLog[1].Wet != 400 {
		t.Fatal("a bad weight changed the flush")
	}
	if c.TotalWet() != 400 || c.TotalDry() != 138.5 {
		t.Fatalf("totals %v %v", c.TotalWet(), c.TotalDry())
	}
	post(h, path+"/delete", url.Values{"n": {"0"}}, nil)
	if len(c.FlushLog) != 1 || c.FlushLog[0].Wet != 400 {
		t.Fatalf("delete: %+v", c.FlushLog)
	}
	// Another site cannot add flushes.
	post(h, path, url.Values{"wet": {"1"}}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if len(c.FlushLog) != 1 {
		t.Fatal("cross-site flush was added")
	}
}

// Notes and remarks save from the culture page, and saving the Edit page
// (which no longer has those boxes) leaves them alone.
func TestNotesSaveInPlace(t *testing.T) {
	freshStore(t)
	c := &Component{Kind: "grow", Species: "x", Strain: "APE"}
	store.Add(c)
	h := newMux()
	post(h, "/component/"+c.ID+"/text", url.Values{"field": {"notes"}, "value": {"Pinned 8/30\r\n"}}, nil)
	post(h, "/component/"+c.ID+"/text", url.Values{"field": {"remarks"}, "value": {"Short and stubby"}}, nil)
	if c.Notes != "Pinned 8/30" || c.Remarks != "Short and stubby" {
		t.Fatalf("got %q / %q", c.Notes, c.Remarks)
	}
	post(h, "/component/"+c.ID, url.Values{"strain": {"APE 2"}, "species": {"x"}}, nil)
	if c.Strain != "APE 2" || c.Notes != "Pinned 8/30" || c.Remarks != "Short and stubby" {
		t.Fatalf("edit page wiped notes: %+v", c)
	}
	r := httptest.NewRequest("GET", "http://localhost:8099/component/"+c.ID, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "Pinned 8/30") || !strings.Contains(w.Body.String(), "Add flush") {
		t.Fatal("culture page is missing the notes box or the flush table")
	}
}

// Statistics never add wet and dry weights together.
func TestStatsKeepWetAndDryApart(t *testing.T) {
	freshStore(t)
	a := &Component{Kind: "grow", Species: "x", FlushLog: []Flush{{Wet: 500, Dry: 50}}}
	b := &Component{Kind: "grow", Species: "x", FlushLog: []Flush{{Wet: 300}}}
	o := &Component{Kind: "grow", Species: "x", Yield: 80}
	store.Add(a)
	store.Add(b)
	store.Add(o)
	s := buildStats()
	if s.TotalWet != 800 || s.TotalDry != 50 || s.WetGrows != 2 || s.DryGrows != 1 {
		t.Fatalf("wet %v/%d dry %v/%d", s.TotalWet, s.WetGrows, s.TotalDry, s.DryGrows)
	}
	if s.UnmarkedGrows != 1 {
		t.Fatalf("unmarked grows %d", s.UnmarkedGrows)
	}
	if s.BestGrow != a || s.BestState != "dry" || s.BestGrams != 50 {
		t.Fatalf("best %+v %v %v", s.BestGrow, s.BestState, s.BestGrams)
	}
}
