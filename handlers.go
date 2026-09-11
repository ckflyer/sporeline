package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var tmpl *template.Template

func loadTemplates() error {
	funcs := template.FuncMap{
		"kinds":  func() []Kind { return Kinds },
		"kindOf": KindOf,
		"cats":   func() []string { return Categories },
		"date": func(s string) string {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return s
			}
			return t.Format("2 Jan 2006")
		},
		"since": func(s string) string {
			t, err := time.Parse("2006-01-02", s)
			if err != nil {
				return ""
			}
			d := int(time.Since(t).Hours() / 24)
			switch {
			case d < 1:
				return "today"
			case d == 1:
				return "1 day"
			case d < 60:
				return fmt.Sprintf("%d days", d)
			default:
				return fmt.Sprintf("%d months", d/30)
			}
		},
		"grams": func(f float64) string {
			if f == 0 {
				return ""
			}
			return strconv.FormatFloat(f, 'f', -1, 64) + " g"
		},
		"children":    func(id string) []*Component { return store.Children(id) },
		"parents":     func(id string) []*Component { return store.Parents(id) },
		"svg":         func(s string) template.HTML { return template.HTML(s) },
		"today":       func() string { return time.Now().Format("2006-01-02") },
		"goneReasons": func() []GoneReason { return GoneReasons },
		// help renders a small ? that explains something genuinely
		// unobvious. Used sparingly; most of the interface should not
		// need it.
		"help": func(text string) template.HTML {
			return template.HTML(`<span class="help" tabindex="0" role="note" data-tip="` +
				template.HTMLEscapeString(text) + `">?</span>`)
		},
		"strainMap": func() template.JS {
			b, _ := json.Marshal(store.StrainSpecies())
			return template.JS(b)
		},
		"goneLabel": GoneReasonLabel,
		"join":      strings.Join,
		"counts": func() map[string]int {
			m := map[string]int{"all": 0, "recipes": len(store.Recipes())}
			for _, k := range Kinds {
				m[k.Key] = 0
			}
			for _, c := range store.All() {
				m[c.Kind]++
				m["all"]++
			}
			return m
		},
		"inc": func(i int) int { return i + 1 },
	}
	t, err := template.New("").Funcs(funcs).ParseFS(tmplFS, "tmpl/*.html")
	if err != nil {
		return err
	}
	tmpl = t
	return nil
}

type page struct {
	Title  string
	Tab    string
	Data   any
	Notice string
}

func render(w http.ResponseWriter, name string, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, p); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	if len(store.All()) == 0 {
		http.Redirect(w, r, "/guide", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/list/all", http.StatusSeeOther)
}

func handleGuide(w http.ResponseWriter, r *http.Request) {
	render(w, "guide.html", page{Title: "How this works", Tab: "guide", Data: store.Dir()})
}

// ---------- lists ----------

type listData struct {
	Kind       Kind
	KindKey    string
	Items      []*Component
	Q          string
	Show       string
	Species    string
	Strain     string
	AllSpecies []string
	AllStrains []string
	Counts     map[string]int
	Total      int
	Pool       int
	Filtered   bool
	Added      []string
	Back       backLink
}

func handleList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	show := r.URL.Query().Get("show")
	species := r.URL.Query().Get("species")
	strain := r.URL.Query().Get("strain")

	var items []*Component
	for _, c := range store.OfKind(kind) {
		if show == "here" && c.Gone {
			continue
		}
		if show == "gone" && !c.Gone {
			continue
		}
		if species != "" && c.Species != species {
			continue
		}
		if strain != "" && c.Strain != strain {
			continue
		}
		if q != "" {
			hay := strings.ToLower(strings.Join([]string{c.ID, c.Strain, c.Species, c.Sub, c.Notes, c.Remarks, c.GoneNote}, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		items = append(items, c)
	}
	counts := map[string]int{"all": 0}
	for _, c := range store.All() {
		counts[c.Kind]++
		counts["all"]++
	}
	rememberList(w, r)
	k := KindOf(kind)
	if kind == "all" {
		k = Kind{Key: "all", Name: "Everything", Plural: "Everything"}
	}
	data := listData{
		Kind: k, KindKey: kind, Items: items, Q: q, Show: show, Species: species, Strain: strain,
		AllSpecies: store.Seen("species"), AllStrains: store.Seen("strain"),
		Counts: counts, Total: len(items), Pool: counts[kind],
		Filtered: q != "" || species != "" || strain != "" || show != "",
		Added:    splitIDs(r.URL.Query().Get("added")),
	}
	// Typing in the search box asks for just the results, not the page.
	if r.Header.Get("X-Partial") != "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "list_results", data); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	render(w, "list.html", page{Title: k.Plural, Tab: kind, Data: data})
}

// ---------- back links ----------

type backLink struct {
	URL   string
	Label string
}

// Back links point at the last list you were actually looking at, not
// at the previous page. Saving an edit returns you to the culture, and
// its back link still says "Grows" — browser history would have sent
// you back into the edit form.
func rememberList(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "lastlist",
		Value:    url.QueryEscape(r.URL.RequestURI()),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func where(r *http.Request, fallback backLink) backLink {
	c, err := r.Cookie("lastlist")
	if err != nil {
		return fallback
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil || !strings.HasPrefix(raw, "/list/") {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	kind := strings.TrimPrefix(u.Path, "/list/")
	label := "Everything"
	if kind != "all" {
		k := KindOf(kind)
		if k.Prefix == "XX" {
			return fallback
		}
		label = k.Plural
	}
	return backLink{URL: raw, Label: label}
}

// ---------- one component ----------

type compData struct {
	Back       backLink
	C          *Component
	Kind       Kind
	Parents    []*Component
	Children   []*Component
	Family     string
	FullGraph  bool
	FamilySize int
	Subs       []string
	AllSpecies []string
	AllStrains []string
	Others     []*Component
	NextKind   Kind
}

func handleComponentEdit(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "component_edit.html", page{Title: "Edit " + c.ID, Tab: c.Kind, Data: compData{
		Back: backLink{URL: "/component/" + c.ID, Label: c.ID},
		C:    c, Kind: KindOf(c.Kind),
		Subs: store.SubsFor(c.Kind), AllSpecies: store.Seen("species"), AllStrains: store.Seen("strain"),
	}})
}

func handleComponent(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	full := r.URL.Query().Get("full") == "1"
	var fam []*Component
	if full {
		fam = store.FullLineage(c.ID)
	} else {
		fam = store.CloseLineage(c.ID)
	}
	var others []*Component
	for _, o := range store.All() {
		if o.ID != c.ID {
			others = append(others, o)
		}
	}
	render(w, "component.html", page{Title: c.ID, Tab: c.Kind, Data: compData{
		Back: where(r, backLink{URL: "/list/" + c.Kind, Label: KindOf(c.Kind).Plural}),
		C:    c, Kind: KindOf(c.Kind),
		Parents: store.Parents(c.ID), Children: store.Children(c.ID),
		Family: store.FamilySVG(fam, c.ID), FullGraph: full, FamilySize: len(fam),
		Subs: store.SubsFor(c.Kind), AllSpecies: store.Seen("species"), AllStrains: store.Seen("strain"),
		Others: others, NextKind: KindOf(KindOf(c.Kind).Next),
	}})
}

func handleComponentUpdate(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	newStrain := strings.TrimSpace(r.FormValue("strain"))
	newSpecies := strings.TrimSpace(r.FormValue("species"))
	oldStrain, oldSpecies := c.Strain, c.Species
	spread := r.FormValue("spread") == "1" &&
		(newStrain != oldStrain || newSpecies != oldSpecies)
	var relatives []*Component
	if spread {
		relatives = store.AllRelatives(c.ID)
	}
	store.Update(func() {
		c.Strain = newStrain
		c.Species = newSpecies
		for _, rel := range relatives {
			if rel.ID == c.ID {
				continue
			}
			if rel.Strain == oldStrain && rel.Species == oldSpecies {
				rel.Strain, rel.Species = newStrain, newSpecies
			}
		}
		c.Sub = strings.TrimSpace(r.FormValue("sub"))
		if d := r.FormValue("created"); d != "" {
			c.Created = d
		}
		c.Notes = r.FormValue("notes")
		c.Remarks = r.FormValue("remarks")
		c.Flushes = strings.TrimSpace(r.FormValue("flushes"))
		if y := strings.TrimSpace(r.FormValue("yield")); y != "" {
			if f, err := strconv.ParseFloat(y, 64); err == nil && f >= 0 {
				c.Yield = f
			}
		} else {
			c.Yield = 0
		}
	})
	http.Redirect(w, r, "/component/"+c.ID, http.StatusSeeOther)
}

func handleGone(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	back := r.FormValue("back") == "1"
	store.Update(func() {
		if back {
			c.Gone, c.GoneAt, c.GoneReason, c.GoneNote = false, "", "", ""
			return
		}
		c.Gone = true
		c.GoneAt = time.Now().Format("2006-01-02")
		if d := r.FormValue("goneAt"); d != "" {
			c.GoneAt = d
		}
		c.GoneReason = r.FormValue("reason")
		if c.GoneReason == "" {
			c.GoneReason = "unknown"
		}
		c.GoneNote = strings.TrimSpace(r.FormValue("goneNote"))
	})
	http.Redirect(w, r, "/component/"+c.ID, http.StatusSeeOther)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c := store.Get(id)
	if c == nil {
		http.NotFound(w, r)
		return
	}
	kind := c.Kind
	store.Delete(id)
	http.Redirect(w, r, "/list/"+kind, http.StatusSeeOther)
}

// ---------- adding ----------

type addData struct {
	Back       backLink
	Strain     string
	AllStrains []string
	Kind       Kind
	Parents    []*Component
	Others     []*Component
	Subs       []string
	AllSpecies []string
	Species    string
	Gen        int
	Today      string
	Prefix     string
	TakenIDs   []string
	Alphabet   string
}

func handleAddForm(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if KindOf(kind).Prefix == "XX" {
		kind = "spores"
	}
	var parents []*Component
	species, strain := "", ""
	for _, pid := range r.URL.Query()["parent"] {
		if p := store.Get(pid); p != nil {
			parents = append(parents, p)
			if species == "" {
				species = p.Species
			}
			if strain == "" {
				strain = p.Strain
			}
		}
	}
	var others []*Component
	for _, o := range store.All() {
		others = append(others, o)
	}
	pids := make([]string, 0, len(parents))
	for _, p := range parents {
		pids = append(pids, p.ID)
	}
	gen := store.GenerationOf(pids)
	render(w, "add.html", page{Title: "Add " + KindOf(kind).Name, Tab: kind, Data: addData{
		Kind: KindOf(kind), Parents: parents, Others: others,
		Subs: store.SubsFor(kind), AllSpecies: store.Seen("species"),
		AllStrains: store.Seen("strain"), Strain: strain,
		Back:    where(r, backLink{URL: "/list/" + kind, Label: KindOf(kind).Plural}),
		Species: species, Gen: gen, Today: time.Now().Format("2006-01-02"),
		Prefix: KindOf(kind).Prefix + string(genChar(gen)), TakenIDs: takenIDs(), Alphabet: tokenChars,
	}})
}

func handleAdd(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	kind := r.FormValue("kind")
	if KindOf(kind).Prefix == "XX" {
		http.Error(w, "unknown kind", 400)
		return
	}
	species := strings.TrimSpace(r.FormValue("species"))
	strain := strings.TrimSpace(r.FormValue("strain"))
	if species == "" && strain == "" {
		species = "Unnamed"
	}
	var parents []string
	seen := map[string]bool{}
	for _, p := range r.Form["parent"] {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p == "" || seen[p] || store.Get(p) == nil {
			continue
		}
		seen[p] = true
		parents = append(parents, p)
	}
	c := &Component{
		Kind:    kind,
		Sub:     strings.TrimSpace(r.FormValue("sub")),
		Strain:  strain,
		Species: species,
		Created: r.FormValue("created"),
		Notes:   r.FormValue("notes"),
		Remarks: r.FormValue("remarks"),
		Parents: parents,
	}
	if y := strings.TrimSpace(r.FormValue("yield")); y != "" {
		if f, err := strconv.ParseFloat(y, 64); err == nil {
			c.Yield = f
		}
	}
	count := 1
	if n, err := strconv.Atoi(r.FormValue("count")); err == nil && n > 1 && n <= 40 {
		count = n
	}
	wanted := r.Form["id"]
	taken := map[string]bool{}
	for _, existing := range store.All() {
		taken[existing.ID] = true
	}
	var made []string
	last := ""
	for i := 0; i < count; i++ {
		cc := *c
		cc.ID = ""
		if i < len(wanted) {
			candidate := strings.ToUpper(strings.TrimSpace(wanted[i]))
			if validID(candidate) && !taken[candidate] {
				cc.ID = candidate
				taken[candidate] = true
			}
		}
		cc.Parents = append([]string{}, parents...)
		if err := store.Add(&cc); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		last = cc.ID
		made = append(made, cc.ID)
	}
	if count > 1 {
		http.Redirect(w, r, "/list/"+kind+"?added="+strings.Join(made, ","), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/component/"+last, http.StatusSeeOther)
}

// ---------- pictures ----------

func handleAddPics(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "that upload was too large", 400)
		return
	}
	for _, fh := range r.MultipartForm.File["pic"] {
		f, err := fh.Open()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 12<<20))
		f.Close()
		if err != nil {
			continue
		}
		_, format, err := image.DecodeConfig(strings.NewReader(string(raw)))
		if err != nil {
			continue // not an image we understand
		}
		name := fmt.Sprintf("%s_%d.%s", c.ID, time.Now().UnixNano(), format)
		if err := os.WriteFile(filepath.Join(store.PicsDir(), name), raw, 0o600); err != nil {
			continue
		}
		store.Update(func() { c.Pics = append(c.Pics, name) })
	}
	http.Redirect(w, r, "/component/"+c.ID, http.StatusSeeOther)
}

func handleDeletePic(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	name := r.FormValue("name")
	store.Update(func() {
		var keep []string
		for _, p := range c.Pics {
			if p == name {
				os.Remove(filepath.Join(store.PicsDir(), filepath.Base(p)))
				continue
			}
			keep = append(keep, p)
		}
		c.Pics = keep
	})
	http.Redirect(w, r, "/component/"+c.ID, http.StatusSeeOther)
}

// ---------- recipes ----------

func handleRecipes(w http.ResponseWriter, r *http.Request) {
	render(w, "recipes.html", page{Title: "Recipes", Tab: "recipes", Data: store.Recipes()})
}

type recipePage struct {
	R    *Recipe
	Back backLink
}

func handleRecipe(w http.ResponseWriter, r *http.Request) {
	rec := store.Recipe(r.PathValue("id"))
	if rec == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "recipe.html", page{Title: rec.Title, Tab: "recipes", Data: recipePage{
		R: rec, Back: where(r, backLink{URL: "/recipes", Label: "Recipes"}),
	}})
}

func handleRecipeForm(w http.ResponseWriter, r *http.Request) {
	var rec *Recipe
	if id := r.URL.Query().Get("id"); id != "" {
		rec = store.Recipe(id)
	}
	if rec == nil {
		rec = &Recipe{}
	}
	for len(rec.Ingredients) < 3 {
		rec.Ingredients = append(rec.Ingredients, Ingredient{})
	}
	render(w, "recipe_form.html", page{Title: "Recipe", Tab: "recipes", Data: rec})
}

func handleRecipeSave(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rec := &Recipe{
		Title: strings.TrimSpace(r.FormValue("title")),
		Yield: strings.TrimSpace(r.FormValue("yield")),
		Notes: r.FormValue("notes"),
	}
	if rec.Title == "" {
		rec.Title = "Untitled recipe"
	}
	amounts, units, names := r.Form["amount"], r.Form["unit"], r.Form["name"]
	for i := range names {
		if strings.TrimSpace(names[i]) == "" {
			continue
		}
		ing := Ingredient{Name: strings.TrimSpace(names[i])}
		if i < len(amounts) {
			ing.Amount = strings.TrimSpace(amounts[i])
		}
		if i < len(units) {
			ing.Unit = units[i]
		}
		rec.Ingredients = append(rec.Ingredients, ing)
	}
	if id := r.FormValue("id"); id != "" {
		if old := store.Recipe(id); old != nil {
			store.Update(func() {
				rec.ID = id
				*old = *rec
			})
			http.Redirect(w, r, "/recipe/"+id, http.StatusSeeOther)
			return
		}
	}
	store.AddRecipe(rec)
	http.Redirect(w, r, "/recipe/"+rec.ID, http.StatusSeeOther)
}

func handleRecipeDelete(w http.ResponseWriter, r *http.Request) {
	store.DeleteRecipe(r.PathValue("id"))
	http.Redirect(w, r, "/recipes", http.StatusSeeOther)
}

// ---------- backup ----------

type dataPage struct {
	S          Settings
	Backups    []backupFile
	BackupsDir string
	Dir        string
	Count      int
	Recipes    int
	Pics       int
	Notice     string
	GuessPath  string
	GuessFound bool
}

func handleDataPage(w http.ResponseWriter, r *http.Request) {
	pics := 0
	for _, c := range store.All() {
		pics += len(c.Pics)
	}
	guess := ""
	if home, err := os.UserHomeDir(); err == nil {
		guess = filepath.Join(home, "mycolog")
	}
	_, statErr := os.Stat(filepath.Join(guess, "mycolog.sqlite3"))
	render(w, "data.html", page{Title: "Your data", Tab: "data", Data: dataPage{
		S: store.Settings(), Backups: store.Backups(), BackupsDir: store.BackupsDir(),
		Dir: store.Dir(), Count: len(store.All()), Recipes: len(store.Recipes()), Pics: pics,
		Notice: r.URL.Query().Get("notice"), GuessPath: guess, GuessFound: statErr == nil,
	}})
}

type settingsPage struct {
	S       Settings
	Backups []backupFile
	Dir     string
	Notice  string
}

func handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	keep, err := strconv.Atoi(r.FormValue("keep"))
	if err != nil || keep < 1 {
		keep = 30
	}
	if keep > 500 {
		keep = 500
	}
	store.SetSettings(func(s *Settings) {
		s.BackupsOn = r.FormValue("backupsOn") == "1"
		s.BackupKeep = keep
	})
	store.PruneBackups()
	http.Redirect(w, r, "/data?notice="+urlEscape("Settings saved."), http.StatusSeeOther)
}

func handleBackupNow(w http.ResponseWriter, r *http.Request) {
	name, err := store.BackupNow("manual")
	notice := "Backed up to " + name + "."
	if err != nil {
		notice = "Backup failed: " + err.Error()
	}
	http.Redirect(w, r, "/data?notice="+urlEscape(notice), http.StatusSeeOther)
}

func handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	notice := "Restored " + name + ". The version from just before this restore was saved first."
	if err := store.RestoreBackup(name); err != nil {
		notice = "Could not restore: " + err.Error()
	}
	http.Redirect(w, r, "/data?notice="+urlEscape(notice), http.StatusSeeOther)
}

func handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	raw, err := os.ReadFile(filepath.Join(store.BackupsDir(), name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(raw)
}

// handleImportAny takes both kinds of import: a Sporeline backup file,
// or another program's folder.
func handleImportAny(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		http.Redirect(w, r, "/data?notice="+urlEscape("That upload was too large."), http.StatusSeeOther)
		return
	}
	switch r.FormValue("source") {
	case "mycolog":
		importFromFolder(w, r)
	default:
		importFromFile(w, r)
	}
}

func importFromFolder(w http.ResponseWriter, r *http.Request) {
	path := r.FormValue("path")
	store.BackupNow("before-import")
	rep, err := ImportLegacy(path)
	var notice string
	if err != nil {
		notice = "Import failed: " + err.Error()
	} else {
		notice = fmt.Sprintf("Imported %d cultures and %d pictures, IDs unchanged.", rep.Cultures, rep.Pictures)
		if rep.Skipped > 0 {
			notice += fmt.Sprintf(" %d were already here.", rep.Skipped)
		}
		if rep.Warning != "" {
			notice += " " + rep.Warning
		}
	}
	http.Redirect(w, r, "/data?notice="+urlEscape(notice), http.StatusSeeOther)
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	d := store.Snapshot()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="sporeline-%s.json"`, time.Now().Format("2006-01-02")))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(d)
}

// bundle is what an import file looks like: the same data, plus any
// pictures carried along as base64 so one file moves everything.
type bundle struct {
	Data
	Pictures map[string]string `json:"pictures,omitempty"` // filename -> base64
}

func importFromFile(w http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/data?notice="+urlEscape("Choose a file first."), http.StatusSeeOther)
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 400<<20))
	if err != nil {
		http.Redirect(w, r, "/data?notice="+urlEscape("Could not read that file."), http.StatusSeeOther)
		return
	}
	var b bundle
	if err := json.Unmarshal(raw, &b); err != nil || b.Components == nil {
		http.Redirect(w, r, "/data?notice="+urlEscape("That is not a Sporeline file."), http.StatusSeeOther)
		return
	}
	store.BackupNow("before-import")
	written := writePictures(b.Pictures)
	var notice string
	if r.FormValue("mode") == "replace" {
		store.Replace(b.Data)
		notice = fmt.Sprintf("Replaced everything: %d cultures, %d pictures.", len(b.Components), written)
	} else {
		n, _ := store.Merge(b.Data)
		notice = fmt.Sprintf("Added %d cultures and %d pictures.", n, written)
	}
	http.Redirect(w, r, "/data?notice="+urlEscape(notice), http.StatusSeeOther)
}

// validID keeps a form-supplied ID to the shape Sporeline issues.
func validID(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(tokenChars+"0", r) && !(r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func takenIDs() []string {
	all := store.All()
	out := make([]string, 0, len(all))
	for _, c := range all {
		out = append(out, c.ID)
	}
	return out
}

func splitIDs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" && store.Get(p) != nil {
			out = append(out, p)
		}
	}
	return out
}

func urlEscape(s string) string {
	return strings.NewReplacer(" ", "+", "&", "%26", "?", "%3F", "#", "%23").Replace(s)
}
