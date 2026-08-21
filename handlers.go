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
		"children": func(id string) []*Component { return store.Children(id) },
		"parents":  func(id string) []*Component { return store.Parents(id) },
		"svg":      func(s string) template.HTML { return template.HTML(s) },
		"today":    func() string { return time.Now().Format("2006-01-02") },
		"join":     strings.Join,
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
	AllSpecies []string
	Counts     map[string]int
	Total      int
	Pool       int
}

func handleList(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	show := r.URL.Query().Get("show")
	species := r.URL.Query().Get("species")

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
		if q != "" {
			hay := strings.ToLower(strings.Join([]string{c.ID, c.Legacy, c.Species, c.Sub, c.Notes, c.Remarks}, " "))
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
	k := KindOf(kind)
	if kind == "all" {
		k = Kind{Key: "all", Name: "Everything", Plural: "Everything"}
	}
	render(w, "list.html", page{Title: k.Plural, Tab: kind, Data: listData{
		Kind: k, KindKey: kind, Items: items, Q: q, Show: show, Species: species,
		AllSpecies: store.Seen("species"), Counts: counts, Total: len(items), Pool: counts[kind],
	}})
}

// ---------- one component ----------

type compData struct {
	C          *Component
	Kind       Kind
	Parents    []*Component
	Children   []*Component
	Family     string
	FullGraph  bool
	FamilySize int
	Subs       []string
	AllSpecies []string
	Others     []*Component
	NextKind   Kind
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
		C: c, Kind: KindOf(c.Kind),
		Parents: store.Parents(c.ID), Children: store.Children(c.ID),
		Family: store.FamilySVG(fam, c.ID), FullGraph: full, FamilySize: len(fam),
		Subs: store.SubsFor(c.Kind), AllSpecies: store.Seen("species"),
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
	newSpecies := strings.TrimSpace(r.FormValue("species"))
	oldSpecies := c.Species
	spreadSpecies := r.FormValue("spread") == "1" && newSpecies != "" && newSpecies != oldSpecies
	var relatives []*Component
	if spreadSpecies {
		relatives = store.AllRelatives(c.ID)
	}
	store.Update(func() {
		c.Species = newSpecies
		for _, rel := range relatives {
			if rel.Species == oldSpecies {
				rel.Species = newSpecies
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
	store.Update(func() {
		c.Gone = !c.Gone
		if c.Gone {
			c.GoneAt = time.Now().Format("2006-01-02")
		} else {
			c.GoneAt = ""
		}
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
	Kind       Kind
	Parents    []*Component
	Others     []*Component
	Subs       []string
	AllSpecies []string
	Species    string
	Gen        int
	Today      string
}

func handleAddForm(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if KindOf(kind).Prefix == "XX" {
		kind = "spores"
	}
	var parents []*Component
	species := ""
	for _, pid := range r.URL.Query()["parent"] {
		if p := store.Get(pid); p != nil {
			parents = append(parents, p)
			if species == "" {
				species = p.Species
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
	render(w, "add.html", page{Title: "Add " + KindOf(kind).Name, Tab: kind, Data: addData{
		Kind: KindOf(kind), Parents: parents, Others: others,
		Subs: store.SubsFor(kind), AllSpecies: store.Seen("species"),
		Species: species, Gen: store.GenerationOf(pids), Today: time.Now().Format("2006-01-02"),
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
	if species == "" {
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
	last := ""
	for i := 0; i < count; i++ {
		cc := *c
		cc.ID = ""
		cc.Parents = append([]string{}, parents...)
		if err := store.Add(&cc); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		last = cc.ID
	}
	if count > 1 {
		http.Redirect(w, r, "/list/"+kind, http.StatusSeeOther)
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

func handleRecipe(w http.ResponseWriter, r *http.Request) {
	rec := store.Recipe(r.PathValue("id"))
	if rec == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "recipe.html", page{Title: rec.Title, Tab: "recipes", Data: rec})
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
	Dir     string
	Count   int
	Recipes int
	Pics    int
	Notice  string
}

func handleDataPage(w http.ResponseWriter, r *http.Request) {
	pics := 0
	for _, c := range store.All() {
		pics += len(c.Pics)
	}
	render(w, "data.html", page{Title: "Your data", Tab: "data", Data: dataPage{
		Dir: store.Dir(), Count: len(store.All()), Recipes: len(store.Recipes()), Pics: pics,
		Notice: r.URL.Query().Get("notice"),
	}})
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

func handleImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		http.Redirect(w, r, "/data?notice=That+file+was+too+large.", http.StatusSeeOther)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/data?notice=Choose+a+file+first.", http.StatusSeeOther)
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 400<<20))
	if err != nil {
		http.Redirect(w, r, "/data?notice=Could+not+read+that+file.", http.StatusSeeOther)
		return
	}
	var b bundle
	if err := json.Unmarshal(raw, &b); err != nil || b.Components == nil {
		http.Redirect(w, r, "/data?notice=That+is+not+a+Sporeline+file.", http.StatusSeeOther)
		return
	}
	written := writePictures(b.Pictures)
	mode := r.FormValue("mode")
	var notice string
	if mode == "replace" {
		store.Replace(b.Data)
		notice = fmt.Sprintf("Replaced everything: %d cultures, %d pictures.", len(b.Components), written)
	} else {
		n, _ := store.Merge(b.Data)
		notice = fmt.Sprintf("Added %d cultures and %d pictures. Anything already here was left alone.", n, written)
	}
	http.Redirect(w, r, "/data?notice="+urlEscape(notice), http.StatusSeeOther)
}

func urlEscape(s string) string {
	return strings.NewReplacer(" ", "+", "&", "%26", "?", "%3F", "#", "%23").Replace(s)
}
