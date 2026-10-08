package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Where things live, and moving them.
//
// The log folder is remembered in a small file outside it (otherwise
// Sporeline would need to know where the log is to find out where the
// log is). The backups folder is remembered in the log's own settings.
//
//	Windows: %APPDATA%\sporeline-location.json
//	macOS:   ~/Library/Application Support/sporeline-location.json
//	Linux:   ~/.config/sporeline-location.json
//
// It sits beside the standard log folder rather than in it (on a Mac the
// two share a parent), so moving the log never moves the bookmark.

// dataFromFlag is set when Sporeline was started with -data, which always
// wins over the remembered location.
var dataFromFlag bool

// storeSwap guards replacing the global store when the log folder moves.
var storeSwap sync.Mutex

func locationFile() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "sporeline-location.json"), nil
}

type location struct {
	DataDir string `json:"dataDir"`
}

// rememberedDataDir returns the folder chosen on the Data page, if any,
// and if it still exists. A missing folder (an unplugged USB drive, say)
// is reported rather than silently replaced by an empty log.
func rememberedDataDir() (string, error) {
	f, err := locationFile()
	if err != nil {
		return "", nil
	}
	raw, err := os.ReadFile(f)
	if err != nil {
		return "", nil
	}
	var loc location
	if json.Unmarshal(raw, &loc) != nil || loc.DataDir == "" {
		return "", nil
	}
	if _, err := os.Stat(loc.DataDir); err != nil {
		return "", fmt.Errorf("your log was moved to %s, but that folder cannot be found. "+
			"If it is on a drive that is not plugged in, plug it in and start Sporeline again. "+
			"To go back to the standard place, delete %s", loc.DataDir, f)
	}
	return loc.DataDir, nil
}

func rememberDataDir(dir string) error {
	f, err := locationFile()
	if err != nil {
		return err
	}
	def, _ := dataDir()
	if samePath(dir, def) {
		// Back in the standard place: nothing to remember.
		err := os.Remove(f)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(location{DataDir: dir}, "", "  ")
	return os.WriteFile(f, b, 0o600)
}

// ---------- checking a folder someone chose ----------

type folderInfo struct {
	Path     string `json:"path"`
	OK       bool   `json:"ok"`
	Problem  string `json:"problem,omitempty"`
	HasLog   bool   `json:"hasLog"`
	Cultures int    `json:"cultures"`
	Same     bool   `json:"same"`
}

// cleanFolder turns what someone typed into an absolute path, or explains
// why it will not do.
func cleanFolder(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), `"'`)
	if p == "" {
		return "", errors.New("Choose a folder first.")
	}
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("Give the whole path, starting from the drive, for example C:\\Users\\you\\Documents\\sporeline.")
	}
	return filepath.Clean(p), nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// inside reports whether child is parent or somewhere below it.
func inside(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		rel2, err := filepath.Rel(strings.ToLower(parent), strings.ToLower(child))
		if err == nil {
			rel = rel2
		}
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// writable makes the folder if needed and checks a file can be written in it.
func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("Could not make that folder: %v", err)
	}
	probe := filepath.Join(dir, ".sporeline-write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("Sporeline is not allowed to save in that folder.")
	}
	os.Remove(probe)
	return nil
}

func inspectDataFolder(raw string) folderInfo {
	p, err := cleanFolder(raw)
	if err != nil {
		return folderInfo{Path: raw, Problem: err.Error()}
	}
	fi := folderInfo{Path: p, OK: true}
	cur := store.Dir()
	switch {
	case samePath(p, cur):
		fi.Same, fi.OK, fi.Problem = true, false, "Your log is already in this folder."
		return fi
	case inside(p, cur):
		fi.OK, fi.Problem = false, "That folder is inside your current log folder. Choose one outside it."
		return fi
	case inside(cur, p) && !samePath(p, cur):
		// Choosing a parent of the current folder is fine only if the log
		// would land beside it, not on top of it. Keep it simple: refuse.
		fi.OK, fi.Problem = false, "Your current log folder is inside that one. Choose a separate folder, or a new folder inside it."
		return fi
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		fi.OK, fi.Problem = false, "That is a file, not a folder."
		return fi
	}
	if b, err := os.ReadFile(filepath.Join(p, "sporeline.json")); err == nil {
		fi.HasLog = true
		var d struct {
			Components []json.RawMessage `json:"components"`
		}
		if json.Unmarshal(b, &d) != nil {
			fi.OK, fi.Problem = false, "That folder has a sporeline.json Sporeline cannot read."
			return fi
		}
		fi.Cultures = len(d.Components)
	}
	return fi
}

func inspectBackupFolder(raw string) folderInfo {
	p, err := cleanFolder(raw)
	if err != nil {
		return folderInfo{Path: raw, Problem: err.Error()}
	}
	fi := folderInfo{Path: p, OK: true}
	if samePath(p, store.BackupsDir()) {
		fi.Same, fi.OK, fi.Problem = true, false, "Backups are already kept here."
	} else if samePath(p, store.Dir()) || inside(p, store.PicsDir()) || inside(p, store.RemovedPicsDir()) {
		fi.OK, fi.Problem = false, "Choose a folder of its own, not the log folder or its pictures."
	} else if st, err := os.Stat(p); err == nil && !st.IsDir() {
		fi.OK, fi.Problem = false, "That is a file, not a folder."
	}
	return fi
}

// ---------- copying ----------

// copyTree copies every file under src into dst, keeping the layout.
// Files already in dst with the same name are left alone.
func copyTree(src, dst string) (int, error) {
	n := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(src, path)
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0o755)
		}
		if _, err := os.Stat(to); err == nil {
			return nil
		}
		if err := copyFile(path, to); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := to + ".copying"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, to)
}

// ---------- moving the log ----------

// MoveDataTo points Sporeline at a new log folder. With an empty folder,
// the current log, pictures and (unless kept elsewhere) backups are
// copied there first. With a folder that already holds a log, Sporeline
// simply switches to it. Either way the old folder is left as it was,
// with a note saying where the log went, so nothing can be lost.
func MoveDataTo(raw string) (string, error) {
	storeSwap.Lock()
	defer storeSwap.Unlock()

	fi := inspectDataFolder(raw)
	if !fi.OK {
		return "", errors.New(fi.Problem)
	}
	if err := writable(fi.Path); err != nil {
		return "", err
	}
	old := store
	oldDir := old.Dir()
	if !fi.HasLog {
		// Write the log itself last, so a half-finished copy is never
		// mistaken for a finished one.
		for _, sub := range []string{"pics", "removed-pics"} {
			if _, err := copyTree(filepath.Join(oldDir, sub), filepath.Join(fi.Path, sub)); err != nil {
				return "", fmt.Errorf("Copying %s failed, so nothing was changed: %v", sub, err)
			}
		}
		if old.Settings().BackupDir == "" {
			if _, err := copyTree(old.BackupsDir(), filepath.Join(fi.Path, "backups")); err != nil {
				return "", fmt.Errorf("Copying backups failed, so nothing was changed: %v", err)
			}
		}
		b, err := json.MarshalIndent(old.Snapshot(), "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(fi.Path, "sporeline.json"), b, 0o600); err != nil {
			return "", fmt.Errorf("Saving the log there failed, so nothing was changed: %v", err)
		}
	}
	next, err := OpenStore(fi.Path)
	if err != nil {
		return "", fmt.Errorf("The log in that folder could not be opened: %v", err)
	}
	if !dataFromFlag {
		if err := rememberDataDir(fi.Path); err != nil {
			return "", fmt.Errorf("Could not remember the new folder: %v", err)
		}
	}
	if !fi.HasLog {
		note := fmt.Sprintf("Sporeline moved this log on %s to:\r\n\r\n    %s\r\n\r\n"+
			"Everything here was copied there and Sporeline now uses that folder.\r\n"+
			"Once you have checked it, you can delete this folder.\r\n",
			time.Now().Format("2 Jan 2006 15:04"), fi.Path)
		os.WriteFile(filepath.Join(oldDir, "MOVED - READ ME.txt"), []byte(note), 0o644)
	}
	store = next
	if fi.HasLog {
		return fmt.Sprintf("Switched to the log in %s (%d cultures). Your previous log was left in %s.", fi.Path, fi.Cultures, oldDir), nil
	}
	return fmt.Sprintf("Your log now lives in %s. The old folder was left as it was; delete it once you have checked everything came across.", fi.Path), nil
}

// ---------- moving backups ----------

// SetBackupDir keeps backups in a different folder (empty means the
// standard backups folder inside the log folder). Existing backups move
// across, so the list and the automatic clean-up keep working.
func (s *Store) SetBackupDir(raw string) (string, error) {
	target := filepath.Join(s.Dir(), "backups")
	if strings.TrimSpace(raw) != "" {
		fi := inspectBackupFolder(raw)
		if !fi.OK {
			return "", errors.New(fi.Problem)
		}
		target = fi.Path
	}
	if samePath(target, s.BackupsDir()) {
		return "Backups are already kept there.", nil
	}
	if err := writable(target); err != nil {
		return "", err
	}
	from := s.BackupsDir()
	moved := 0
	for _, b := range s.Backups() {
		src, dst := filepath.Join(from, b.Name), filepath.Join(target, b.Name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if os.Rename(src, dst) != nil { // another drive: copy, then remove
			if err := copyFile(src, dst); err != nil {
				return "", fmt.Errorf("Moving %s failed: %v. Backups stay where they were.", b.Name, err)
			}
			os.Chtimes(dst, b.When, b.When) // keep the date the list sorts by
			os.Remove(src)
		}
		moved++
	}
	keep := ""
	if !samePath(target, filepath.Join(s.Dir(), "backups")) {
		keep = target
	}
	s.SetSettings(func(st *Settings) { st.BackupDir = keep })
	s.PruneBackups()
	return fmt.Sprintf("Backups are now kept in %s. %d moved across.", target, moved), nil
}

// ---------- the computer's own windows ----------

// openFolder shows a folder in Explorer, Finder or the file manager.
// A variable so tests can see what would have opened.
var openFolder = func(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // explorer exits 1 even when it worked; nothing to check
	return nil
}

// errNoPicker means the computer has no folder chooser Sporeline can use;
// the page then just asks for the path to be typed.
var errNoPicker = errors.New("no folder chooser")

// pickFolder shows the computer's own "choose a folder" window and waits
// for an answer. An empty result means it was cancelled.
var pickFolder = func(start string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// A hidden, always-on-top owner window keeps the chooser in front
		// of the browser instead of opening behind it.
		ps := `[Console]::OutputEncoding=[Text.Encoding]::UTF8
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Choose a folder for Sporeline'
$d.ShowNewFolderButton = $true
if ($env:SL_START -and (Test-Path -LiteralPath $env:SL_START)) { $d.SelectedPath = $env:SL_START }
$o = New-Object System.Windows.Forms.Form
$o.TopMost = $true
if ($d.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.SelectedPath) }`
		// Sent encoded, so quotes and line breaks reach PowerShell intact.
		u := utf16.Encode([]rune(ps))
		b := make([]byte, 2*len(u))
		for i, c := range u {
			binary.LittleEndian.PutUint16(b[2*i:], c)
		}
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA",
			"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
		cmd.Env = append(os.Environ(), "SL_START="+start)
	case "darwin":
		cmd = exec.Command("osascript", "-e",
			`POSIX path of (choose folder with prompt "Choose a folder for Sporeline")`)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command(p, "--file-selection", "--directory", "--title=Choose a folder for Sporeline", "--filename="+start+"/")
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.Command(p, "--getexistingdirectory", start)
		} else {
			return "", errNoPicker
		}
	}
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil // cancelled
		}
		return "", errNoPicker
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), `/\`+"\r\n"), nil
}
