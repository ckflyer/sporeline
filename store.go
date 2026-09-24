package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- types ----------

// The four top level kinds, mirroring mycolog. The prefix is what shows
// up at the front of every ID, so it is what you read off the tape.
type Kind struct {
	Key    string
	Prefix string
	Name   string // singular
	Plural string
	Color  string
	Next   string // what you usually make from it
	Subs   []string
}

var Kinds = []Kind{
	{"spores", "SE", "Spores", "Spores", "#6E5AA6", "myc",
		[]string{"Print", "Syringe", "Swab", "Wild collection"}},
	{"myc", "MY", "Mycelium", "Mycelium", "#2F8F72", "spawn",
		[]string{"Agar plate", "Liquid culture", "Slant", "Clone", "Grain to agar"}},
	{"spawn", "SN", "Spawn", "Spawn", "#C09A1F", "grow",
		[]string{"Rye", "Oats", "Wheat", "Millet", "Sawdust", "Master grain"}},
	{"grow", "GR", "Grow", "Grows", "#A54E29", "grow",
		[]string{"Monotub", "Bag", "Bucket", "Tray", "Log", "Outdoor bed"}},
}

// Title is what to call this entry. A strain name is the thing you
// actually say out loud, so it leads; species is the formal backing.
func (c *Component) Title() string {
	if c.Strain != "" {
		return c.Strain
	}
	if c.Species != "" {
		return c.Species
	}
	return "Unnamed"
}

// Sub is the quieter half of the name, empty when there is nothing to add.
func (c *Component) Subtitle() string {
	if c.Strain != "" && c.Species != "" {
		return c.Species
	}
	return ""
}

func KindOf(key string) Kind {
	for _, k := range Kinds {
		if k.Key == key {
			return k
		}
	}
	return Kind{Key: key, Prefix: "XX", Name: key, Plural: key, Color: "#6B7566"}
}

type Component struct {
	ID         string   `json:"id"` // SE1K7P — prefix, generation, three random
	Kind       string   `json:"kind"`
	Sub        string   `json:"sub,omitempty"`
	Strain     string   `json:"strain,omitempty"` // what you call it; leads the display
	Species    string   `json:"species"`          // the formal name behind it
	Gen        int      `json:"gen"`
	Created    string   `json:"created"` // YYYY-MM-DD
	Notes      string   `json:"notes,omitempty"`
	Remarks    string   `json:"remarks,omitempty"` // genetic remarks, grows
	Yield      float64  `json:"yield,omitempty"`   // grams, grows
	Flushes    string   `json:"flushes,omitempty"` // free text: "1st 340g, 2nd 190g"
	Gone       bool     `json:"gone"`
	GoneAt     string   `json:"goneAt,omitempty"`
	GoneReason string   `json:"goneReason,omitempty"` // used / contaminated / discarded / lost / unknown
	GoneNote   string   `json:"goneNote,omitempty"`   // what actually happened
	Parents    []string `json:"parents"`
	Pics       []string `json:"pics,omitempty"` // filenames inside pics/
	Added      int64    `json:"added"`
}

type Ingredient struct {
	Amount string `json:"amount"`
	Unit   string `json:"unit"`
	Name   string `json:"name"`
}

type Recipe struct {
	Title       string       `json:"title"`
	ID          string       `json:"id"`
	Category    string       `json:"category"`
	Yield       string       `json:"yield,omitempty"`
	Ingredients []Ingredient `json:"ingredients,omitempty"`
	Sterilize   string       `json:"sterilize,omitempty"`
	Steps       string       `json:"steps,omitempty"`
	Notes       string       `json:"notes,omitempty"`
}

var Categories = []string{"Agar", "Liquid culture", "Grain", "Substrate", "Other"}

// Why something is no longer around. Kept as its own field rather than
// buried in notes, so a season of entries can answer questions like
// which genetic contaminates most.
type GoneReason struct {
	Key   string
	Label string
	Color string
}

var GoneReasons = []GoneReason{
	{"used", "Used up", "#63715F"},
	{"contaminated", "Contaminated", "#9A3A2C"},
	{"discarded", "Thrown out", "#8A7A53"},
	{"lost", "Died or lost", "#6B7566"},
	{"unknown", "Not recorded", "#9AA394"},
}

func GoneReasonLabel(key string) string {
	for _, r := range GoneReasons {
		if r.Key == key {
			return r.Label
		}
	}
	return "Gone"
}

type Settings struct {
	BackupsOn  bool   `json:"backupsOn"`
	BackupKeep int    `json:"backupKeep"`
	LastBackup string `json:"lastBackup,omitempty"`
}

func defaultSettings() Settings {
	return Settings{BackupsOn: true, BackupKeep: 30}
}

type Data struct {
	Version    int          `json:"version"`
	Components []*Component `json:"components"`
	Recipes    []*Recipe    `json:"recipes"`
	Settings   Settings     `json:"settings"`
}

// ---------- store ----------

type Store struct {
	mu   sync.RWMutex
	dir  string
	file string
	data Data
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "pics"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "backups"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, file: filepath.Join(dir, "sporeline.json")}
	s.data = Data{Version: 1, Settings: defaultSettings()}
	b, err := os.ReadFile(s.file)
	if err == nil {
		if err := json.Unmarshal(b, &s.data); err != nil {
			// Never clobber a file we cannot read: park it and start clean.
			backup := s.file + ".unreadable-" + time.Now().Format("20060102-150405")
			os.Rename(s.file, backup)
			return nil, fmt.Errorf("could not read %s (moved to %s): %w", s.file, backup, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.data.Settings.BackupKeep == 0 {
		s.data.Settings = defaultSettings()
	}
	return s, nil
}

func (s *Store) PicsDir() string    { return filepath.Join(s.dir, "pics") }
func (s *Store) BackupsDir() string { return filepath.Join(s.dir, "backups") }
func (s *Store) Dir() string        { return s.dir }

// flush writes atomically: temp file, then rename over the original.
func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

func (s *Store) All() []*Component {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Component, len(s.data.Components))
	copy(out, s.data.Components)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		return out[i].Added > out[j].Added
	})
	return out
}

func (s *Store) Get(id string) *Component {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.data.Components {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func (s *Store) OfKind(kind string) []*Component {
	var out []*Component
	for _, c := range s.All() {
		if kind == "all" || c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func (s *Store) Children(id string) []*Component {
	var out []*Component
	for _, c := range s.All() {
		for _, p := range c.Parents {
			if p == id {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (s *Store) Parents(id string) []*Component {
	c := s.Get(id)
	if c == nil {
		return nil
	}
	var out []*Component
	for _, p := range c.Parents {
		if pc := s.Get(p); pc != nil {
			out = append(out, pc)
		}
	}
	return out
}

// Seen returns every distinct value entered before for a field, so the
// forms can suggest without pre-loading anything you did not type.
func (s *Store) Seen(field string) []string {
	set := map[string]bool{}
	for _, c := range s.All() {
		switch field {
		case "species":
			if c.Species != "" {
				set[c.Species] = true
			}
		case "strain":
			if c.Strain != "" {
				set[c.Strain] = true
			}
		case "sub":
			if c.Sub != "" {
				set[c.Sub] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StrainSpecies remembers which species you paired a strain with, so
// typing a strain you have used before fills the species in for you.
func (s *Store) StrainSpecies() map[string]string {
	out := map[string]string{}
	for _, c := range s.All() { // All() is newest first, so keep the first hit
		if c.Strain == "" || c.Species == "" {
			continue
		}
		if _, seen := out[c.Strain]; !seen {
			out[c.Strain] = c.Species
		}
	}
	return out
}

func (s *Store) SubsFor(kind string) []string {
	set := map[string]bool{}
	var out []string
	for _, d := range KindOf(kind).Subs {
		if !set[d] {
			set[d] = true
			out = append(out, d)
		}
	}
	for _, c := range s.All() {
		if c.Kind == kind && c.Sub != "" && !set[c.Sub] {
			set[c.Sub] = true
			out = append(out, c.Sub)
		}
	}
	return out
}

// ---------- IDs ----------

// Characters chosen to survive a Sharpie and a cold shed: no I, O or 0.
const tokenChars = "ABCDEFGHJKLMNPQRSTUVWXYZ123456789"

// genChar keeps the ID six characters long however deep the lineage
// goes: 0-9 then A, B, C… for generation ten and beyond.
func genChar(gen int) byte {
	if gen < 0 {
		gen = 0
	}
	if gen <= 9 {
		return byte('0' + gen)
	}
	i := gen - 10
	if i > 25 {
		return 'Z'
	}
	return byte('A' + i)
}

func (s *Store) NewID(kind string, gen int) string {
	taken := map[string]bool{}
	s.mu.RLock()
	for _, c := range s.data.Components {
		taken[c.ID] = true
	}
	s.mu.RUnlock()
	prefix := KindOf(kind).Prefix + string(genChar(gen))
	for try := 0; try < 10000; try++ {
		b := []byte(prefix)
		for i := 0; i < 3; i++ {
			b = append(b, tokenChars[rand.Intn(len(tokenChars))])
		}
		id := string(b)
		if !taken[id] {
			return id
		}
	}
	return prefix + fmt.Sprint(time.Now().UnixNano()%1000)
}

// GenerationOf is one more than the deepest parent. Nothing above it
// means generation zero: bought, gifted, or picked up in the woods.
func (s *Store) GenerationOf(parentIDs []string) int {
	best := -1
	for _, p := range parentIDs {
		if c := s.Get(p); c != nil && c.Gen > best {
			best = c.Gen
		}
	}
	return best + 1
}

// ---------- mutations ----------

func (s *Store) Add(c *Component) error {
	c.Gen = s.GenerationOf(c.Parents)
	if c.ID == "" {
		c.ID = s.NewID(c.Kind, c.Gen)
	}
	c.Added = time.Now().UnixNano()
	if c.Created == "" {
		c.Created = time.Now().Format("2006-01-02")
	}
	s.mu.Lock()
	s.data.Components = append(s.data.Components, c)
	err := s.flush()
	s.mu.Unlock()
	return err
}

// AddImported keeps entries exactly as they arrived: their own IDs, their
// own generations. Nothing is renamed on the way in. They go in all
// together with a single save, so an import either lands whole or not
// at all, and a big log does not rewrite the file once per culture.
func (s *Store) AddImported(list []*Component) error {
	now := time.Now().UnixNano()
	for i, c := range list {
		if c.ID == "" {
			return fmt.Errorf("imported entry has no ID")
		}
		if c.Added == 0 {
			c.Added = now + int64(i)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.data.Components)
	s.data.Components = append(s.data.Components, list...)
	if err := s.flush(); err != nil {
		s.data.Components = s.data.Components[:before]
		return err
	}
	return nil
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

func (s *Store) SetSettings(fn func(*Settings)) error {
	s.mu.Lock()
	fn(&s.data.Settings)
	err := s.flush()
	s.mu.Unlock()
	return err
}

func (s *Store) Update(fn func()) error {
	s.mu.Lock()
	fn()
	err := s.flush()
	s.mu.Unlock()
	return err
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []*Component
	for _, c := range s.data.Components {
		if c.ID == id {
			for _, p := range c.Pics {
				s.retirePic(p)
			}
			continue
		}
		var pr []string
		for _, p := range c.Parents {
			if p != id {
				pr = append(pr, p)
			}
		}
		c.Parents = pr
		keep = append(keep, c)
	}
	s.data.Components = keep
	return s.flush()
}

func (s *Store) Recipes() []*Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Recipe, len(s.data.Recipes))
	copy(out, s.data.Recipes)
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

func (s *Store) Recipe(id string) *Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.data.Recipes {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (s *Store) AddRecipe(r *Recipe) error {
	s.mu.Lock()
	r.ID = fmt.Sprintf("r%d", time.Now().UnixNano())
	s.data.Recipes = append(s.data.Recipes, r)
	err := s.flush()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteRecipe(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []*Recipe
	for _, r := range s.data.Recipes {
		if r.ID != id {
			keep = append(keep, r)
		}
	}
	s.data.Recipes = keep
	return s.flush()
}

func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

func (s *Store) Replace(d Data) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.Version == 0 {
		d.Version = 1
	}
	s.data = d
	return s.flush()
}

func (s *Store) Merge(d Data) (added int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	have := map[string]bool{}
	for _, c := range s.data.Components {
		have[c.ID] = true
	}
	for _, c := range d.Components {
		if have[c.ID] {
			continue
		}
		s.data.Components = append(s.data.Components, c)
		have[c.ID] = true
		added++
	}
	haveR := map[string]bool{}
	for _, r := range s.data.Recipes {
		haveR[r.ID] = true
	}
	for _, r := range d.Recipes {
		if !haveR[r.ID] {
			s.data.Recipes = append(s.data.Recipes, r)
		}
	}
	return added, s.flush()
}
