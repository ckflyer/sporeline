package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backups are plain copies of sporeline.json, named by the moment they
// were taken. They cost a few kilobytes each and they are the difference
// between a bad import being an annoyance and being a disaster.

type backupFile struct {
	Name string
	When time.Time
	Size int64
}

func (s *Store) BackupNow(tag string) (string, error) {
	d := s.Snapshot()
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	stamp := time.Now().Format("2006-01-02-150405")
	name := fmt.Sprintf("sporeline-%s.json", stamp)
	if tag != "" {
		name = fmt.Sprintf("sporeline-%s-%s.json", stamp, tag)
	}
	path := filepath.Join(s.BackupsDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	s.SetSettings(func(st *Settings) { st.LastBackup = time.Now().Format("2006-01-02 15:04") })
	s.PruneBackups()
	return name, nil
}

func (s *Store) Backups() []backupFile {
	entries, err := os.ReadDir(s.BackupsDir())
	if err != nil {
		return nil
	}
	var out []backupFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, backupFile{Name: e.Name(), When: info.ModTime(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out
}

func (s *Store) PruneBackups() {
	keep := s.Settings().BackupKeep
	if keep <= 0 {
		keep = 30
	}
	list := s.Backups()
	for i := keep; i < len(list); i++ {
		os.Remove(filepath.Join(s.BackupsDir(), list[i].Name))
	}
	s.CleanRemovedPics()
}

// BackupDaily takes one backup per day, on startup. Enough to undo a
// mistake you notice a week later, quiet enough that you never think
// about it.
func (s *Store) BackupDaily() {
	st := s.Settings()
	if !st.BackupsOn || len(s.All()) == 0 {
		return
	}
	today := time.Now().Format("2006-01-02")
	for _, b := range s.Backups() {
		if b.When.Format("2006-01-02") == today {
			return
		}
	}
	s.BackupNow("")
}

func (s *Store) RestoreBackup(name string) error {
	name = filepath.Base(name)
	raw, err := os.ReadFile(filepath.Join(s.BackupsDir(), name))
	if err != nil {
		return err
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return fmt.Errorf("that backup file could not be read")
	}
	if d.Components == nil {
		return fmt.Errorf("that backup holds no cultures")
	}
	// Pictures first: taking the before-restore backup can prune the very
	// backup being restored, and with it the last mention of its pictures.
	s.bringBackPics(d)
	s.BackupNow("before-restore")
	settings := s.Settings()
	d.Settings = settings // keep current preferences, not the old ones
	return s.Replace(d)
}
