package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keeps the location bookmark out of the real config folder.
func isolateConfig(t *testing.T) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dataFromFlag = false
}

func seeded(t *testing.T) (*Component, string) {
	t.Helper()
	freshStore(t)
	c := &Component{Kind: "grow", Species: "Oyster", Pics: []string{"p_1.png"}}
	store.Add(c)
	os.WriteFile(filepath.Join(store.PicsDir(), "p_1.png"), []byte("img"), 0o600)
	name, err := store.BackupNow("manual")
	if err != nil {
		t.Fatal(err)
	}
	return c, name
}

func TestMoveLogToEmptyFolder(t *testing.T) {
	isolateConfig(t)
	c, backup := seeded(t)
	old := store.Dir()
	to := filepath.Join(t.TempDir(), "new place")

	msg, err := MoveDataTo(to)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, to) || store.Dir() != to {
		t.Fatalf("store is at %s, message %q", store.Dir(), msg)
	}
	if store.Get(c.ID) == nil {
		t.Fatal("culture did not come across")
	}
	for _, f := range []string{"sporeline.json", "pics/p_1.png", "backups/" + backup} {
		if _, err := os.Stat(filepath.Join(to, f)); err != nil {
			t.Errorf("%s missing in new folder", f)
		}
	}
	// The old folder is left whole, with a note.
	if _, err := os.Stat(filepath.Join(old, "sporeline.json")); err != nil {
		t.Error("old log should be left in place")
	}
	if _, err := os.Stat(filepath.Join(old, "MOVED - READ ME.txt")); err != nil {
		t.Error("old folder should say where the log went")
	}
	// Next start finds it.
	got, err := rememberedDataDir()
	if err != nil || got != to {
		t.Fatalf("remembered %q, %v", got, err)
	}
	// Moving back to the standard place forgets the bookmark.
	def, _ := dataDir()
	if _, err := MoveDataTo(def); err != nil {
		t.Fatal(err)
	}
	if got, _ := rememberedDataDir(); got != "" {
		t.Fatalf("bookmark should be gone, got %q", got)
	}
}

func TestSwitchToExistingLog(t *testing.T) {
	isolateConfig(t)
	seeded(t)
	other := t.TempDir()
	o, _ := OpenStore(other)
	o.Add(&Component{Kind: "myc", Species: "Lion's Mane"})
	o.Add(&Component{Kind: "myc", Species: "Lion's Mane"})

	fi := inspectDataFolder(other)
	if !fi.OK || !fi.HasLog || fi.Cultures != 2 {
		t.Fatalf("inspect: %+v", fi)
	}
	if _, err := MoveDataTo(other); err != nil {
		t.Fatal(err)
	}
	if len(store.All()) != 2 {
		t.Fatalf("should now show the other log, has %d", len(store.All()))
	}
}

func TestBadFoldersRefused(t *testing.T) {
	isolateConfig(t)
	freshStore(t)
	cur := store.Dir()
	for _, p := range []string{"", "relative/path", cur, filepath.Join(cur, "pics"), filepath.Dir(cur)} {
		if fi := inspectDataFolder(p); fi.OK {
			t.Errorf("%q should be refused", p)
		}
		if _, err := MoveDataTo(p); err == nil {
			t.Errorf("move to %q should fail", p)
		}
	}
	if store.Dir() != cur {
		t.Fatal("a refused move must not change anything")
	}
	f := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(f, []byte("x"), 0o600)
	if inspectDataFolder(f).OK || inspectBackupFolder(f).OK {
		t.Error("a file is not a folder")
	}
	if inspectBackupFolder(cur).OK || inspectBackupFolder(filepath.Join(cur, "pics")).OK {
		t.Error("backups must not go in the log folder itself or the pictures")
	}
}

func TestMoveBackupsAndBack(t *testing.T) {
	isolateConfig(t)
	_, name := seeded(t)
	away := filepath.Join(t.TempDir(), "sporeline backups")
	if _, err := store.SetBackupDir(away); err != nil {
		t.Fatal(err)
	}
	if store.BackupsDir() != away || len(store.Backups()) != 1 {
		t.Fatalf("backups at %s, %d listed", store.BackupsDir(), len(store.Backups()))
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), "backups", name)); err == nil {
		t.Error("backup should have moved, not been copied")
	}
	store.BackupNow("manual")
	if len(store.Backups()) != 2 {
		t.Fatal("new backups should land in the new folder")
	}
	// Survives a restart.
	again, _ := OpenStore(store.Dir())
	if again.BackupsDir() != away {
		t.Fatal("backup folder not remembered")
	}
	// A log moved elsewhere leaves custom backups where they are.
	to := filepath.Join(t.TempDir(), "log")
	if _, err := MoveDataTo(to); err != nil {
		t.Fatal(err)
	}
	if store.BackupsDir() != away {
		t.Fatal("custom backup folder should stay put when the log moves")
	}
	// Back to standard.
	if _, err := store.SetBackupDir(""); err != nil {
		t.Fatal(err)
	}
	if store.BackupsDir() != filepath.Join(to, "backups") || len(store.Backups()) != 2 {
		t.Fatalf("back to standard: %s, %d", store.BackupsDir(), len(store.Backups()))
	}
}

// A backup folder on a drive that has gone must not stop Sporeline
// starting; it falls back to the standard one.
func TestMissingBackupDriveFallsBack(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(t.TempDir(), "notadir")
	os.WriteFile(blocker, []byte("x"), 0o600)
	b, _ := json.Marshal(Data{Version: 1, Settings: Settings{BackupsOn: true, BackupKeep: 30, BackupDir: filepath.Join(blocker, "sub")}})
	os.WriteFile(filepath.Join(dir, "sporeline.json"), b, 0o600)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.BackupsDir() != filepath.Join(dir, "backups") {
		t.Fatalf("fell back to %s", s.BackupsDir())
	}
}

func TestFolderEndpoints(t *testing.T) {
	isolateConfig(t)
	seeded(t)
	var opened string
	openFolder = func(d string) error { opened = d; return nil }
	picked := ""
	pickFolder = func(string) (string, error) { return picked, nil }

	mux := http.NewServeMux()
	mux.HandleFunc("POST /folder/open", handleFolderOpen)
	mux.HandleFunc("POST /folder/pick", handleFolderPick)
	mux.HandleFunc("GET /folder/check", handleFolderCheck)
	mux.HandleFunc("POST /folder/move", handleFolderMove)
	mux.HandleFunc("GET /backups.json", handleBackupsList)
	h := guard(mux)

	post(h, "/folder/open", url.Values{"which": {"backups"}}, nil)
	if opened != store.BackupsDir() {
		t.Fatalf("opened %q", opened)
	}
	post(h, "/folder/open", url.Values{"which": {"data"}}, nil)
	if opened != store.Dir() {
		t.Fatalf("opened %q", opened)
	}
	// Another website cannot open folders.
	opened = ""
	post(h, "/folder/open", url.Values{"which": {"data"}}, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if opened != "" {
		t.Fatal("cross-site request should be refused")
	}

	// Picker cancelled, then a folder chosen.
	w := post(h, "/folder/pick", url.Values{"which": {"data"}}, nil)
	if !strings.Contains(w.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel: %s", w.Body.String())
	}
	picked = filepath.Join(t.TempDir(), "x")
	w = post(h, "/folder/pick", url.Values{"which": {"data"}}, nil)
	var fi folderInfo
	json.Unmarshal(w.Body.Bytes(), &fi)
	if !fi.OK || fi.Path != picked {
		t.Fatalf("pick: %s", w.Body.String())
	}

	// Restore list.
	r := httptest.NewRequest("GET", "http://localhost:8099/backups.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var rows []backupRow
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Cultures != 1 || rows[0].Why != "Backed up by hand" {
		t.Fatalf("rows: %s", rec.Body.String())
	}

	// Move through the form.
	w = post(h, "/folder/move", url.Values{"which": {"data"}, "path": {picked}}, nil)
	if store.Dir() != picked {
		t.Fatalf("move via form failed: %s", w.Header().Get("Location"))
	}
}

func TestDataPageRenders(t *testing.T) {
	isolateConfig(t)
	seeded(t)
	r := httptest.NewRequest("GET", "http://localhost:8099/data", nil)
	w := httptest.NewRecorder()
	handleDataPage(w, r)
	body := w.Body.String()
	for _, want := range []string{"Open folder", "Restore…", "Kept in", `id="folderdlg"`, `id="restore"`} {
		if !strings.Contains(body, want) {
			t.Errorf("data page missing %q", want)
		}
	}
	if strings.Count(body, `class="panel"`) > 4 {
		t.Error("backup panels should be combined")
	}
}

func TestBackupWhy(t *testing.T) {
	for name, want := range map[string]string{
		"sporeline-2026-10-08-163600.json":                 "Daily backup",
		"sporeline-2026-10-08-163600-manual.json":          "Backed up by hand",
		"sporeline-2026-10-08-163600-manual-3.json":        "Backed up by hand",
		"sporeline-2026-10-08-163600-before-import-2.json": "Before an import",
	} {
		if got := backupWhy(name); got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

func TestRestoreEmptyBackup(t *testing.T) {
	freshStore(t)
	name, _ := store.BackupNow("before-import")
	store.Add(&Component{Kind: "myc", Species: "X"})
	if err := store.RestoreBackup(name); err != nil {
		t.Fatal(err)
	}
	if len(store.All()) != 0 {
		t.Fatal("restoring an empty backup should empty the log")
	}
	os.WriteFile(filepath.Join(store.BackupsDir(), "junk.json"), []byte(`{"hello":1}`), 0o600)
	if err := store.RestoreBackup("junk.json"); err == nil {
		t.Fatal("a file that is not Sporeline's must be refused")
	}
}
