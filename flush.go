package main

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Flush is one harvest from a grow. Growers usually weigh a flush wet
// when they pick it and again once it has dried, so each flush keeps
// both weights. Either can be left blank.
type Flush struct {
	Date string  `json:"date,omitempty"` // YYYY-MM-DD
	Wet  float64 `json:"wet,omitempty"`  // grams
	Dry  float64 `json:"dry,omitempty"`  // grams
	// Unmarked holds a weight recorded before Sporeline asked whether it
	// was wet or dry. The culture page asks once and moves it across.
	Unmarked float64 `json:"unmarked,omitempty"`
	Note     string  `json:"note,omitempty"`
}

func (c *Component) TotalWet() (g float64) {
	for _, f := range c.FlushLog {
		g += f.Wet
	}
	return
}

func (c *Component) TotalDry() (g float64) {
	for _, f := range c.FlushLog {
		g += f.Dry
	}
	return
}

func (c *Component) TotalUnmarked() (g float64) {
	for _, f := range c.FlushLog {
		g += f.Unmarked
	}
	return
}

// upgradeHarvest turns the old single "total yield" number and the old
// free-text flushes line into one flush entry, so nothing recorded under
// earlier versions (or brought in from mycolog) is lost. The weight is
// kept as unmarked because nobody said whether it was wet or dry.
func (c *Component) upgradeHarvest() {
	if c.Yield <= 0 && strings.TrimSpace(c.Flushes) == "" {
		c.Yield, c.Flushes = 0, ""
		return
	}
	note := ""
	if c.Yield > 0 {
		note = "This was the grow's total yield, so it may cover more than one flush."
	}
	if t := strings.TrimSpace(c.Flushes); t != "" {
		note = strings.TrimSpace(note + " Your old flush notes: " + t)
	}
	old := Flush{Unmarked: c.Yield, Note: note}
	c.FlushLog = append([]Flush{old}, c.FlushLog...)
	c.Yield, c.Flushes = 0, ""
}

// parseGrams reads a weight the way people type it: "12.5", "12,5",
// "12 g". Blank means none. Negative or unreadable is an error.
func parseGrams(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, unit := range []string{"grams", "gram", "g"} {
		if strings.HasSuffix(s, unit) {
			s = strings.TrimSpace(strings.TrimSuffix(s, unit))
			break
		}
	}
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, true
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f > 1e6 {
		return 0, false
	}
	return f, true
}

// ---------- saving straight from the culture page ----------

// backToCulture returns to the culture page at the section just saved,
// with a short message there, keeping the whole-tree view if it was on.
func backToCulture(w http.ResponseWriter, r *http.Request, id, anchor, msg string) {
	q := url.Values{}
	if msg != "" {
		q.Set("msg", msg)
		q.Set("at", anchor)
	}
	if r.FormValue("full") == "1" {
		q.Set("full", "1")
	}
	u := "/component/" + id
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	http.Redirect(w, r, u+"#"+anchor, http.StatusSeeOther)
}

// flushIndex reads which flush a form is about. ok is false when the
// number does not point at an existing flush.
func flushIndex(c *Component, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.FormValue("n"))
	return n, err == nil && n >= 0 && n < len(c.FlushLog)
}

// handleFlushSave adds a new flush, or saves changes to an existing one
// when the form carries its number.
func handleFlushSave(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	wet, okW := parseGrams(r.FormValue("wet"))
	dry, okD := parseGrams(r.FormValue("dry"))
	if !okW || !okD {
		backToCulture(w, r, c.ID, "harvest", "That weight could not be read. Use a number of grams, like 42.5.")
		return
	}
	date := strings.TrimSpace(r.FormValue("date"))
	note := strings.TrimSpace(r.FormValue("note"))
	msg := ""
	store.Update(func() {
		if r.FormValue("n") == "" {
			if wet == 0 && dry == 0 {
				msg = "Enter a wet or dry weight to add a flush."
				return
			}
			c.FlushLog = append(c.FlushLog, Flush{Date: date, Wet: wet, Dry: dry, Note: note})
			msg = fmt.Sprintf("Flush %d added.", len(c.FlushLog))
			return
		}
		n, ok := flushIndex(c, r)
		if !ok {
			msg = "That flush no longer exists. The page has been refreshed."
			return
		}
		f := &c.FlushLog[n]
		f.Date, f.Wet, f.Dry = date, wet, dry
		if _, sent := r.Form["note"]; sent {
			f.Note = note
		}
		msg = fmt.Sprintf("Flush %d saved.", n+1)
	})
	backToCulture(w, r, c.ID, "harvest", msg)
}

func handleFlushDelete(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	msg := ""
	store.Update(func() {
		n, ok := flushIndex(c, r)
		if !ok {
			return
		}
		c.FlushLog = append(c.FlushLog[:n], c.FlushLog[n+1:]...)
		msg = fmt.Sprintf("Flush %d removed.", n+1)
	})
	backToCulture(w, r, c.ID, "harvest", msg)
}

// handleFlushMark answers "was that old weight wet or dry?" and moves it
// into the right column.
func handleFlushMark(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	as := r.FormValue("as")
	store.Update(func() {
		n, ok := flushIndex(c, r)
		if !ok || (as != "wet" && as != "dry") {
			return
		}
		f := &c.FlushLog[n]
		if as == "wet" {
			f.Wet += f.Unmarked
		} else {
			f.Dry += f.Unmarked
		}
		f.Unmarked = 0
	})
	backToCulture(w, r, c.ID, "harvest", "Saved.")
}

// handleTextSave saves the genetic remarks or the notes box.
func handleTextSave(w http.ResponseWriter, r *http.Request) {
	c := store.Get(r.PathValue("id"))
	if c == nil {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	field := r.FormValue("field")
	value := strings.TrimRight(strings.ReplaceAll(r.FormValue("value"), "\r\n", "\n"), " \n")
	switch field {
	case "remarks":
		store.Update(func() { c.Remarks = value })
		backToCulture(w, r, c.ID, "remarks", "Genetic remarks saved.")
	case "notes":
		store.Update(func() { c.Notes = value })
		backToCulture(w, r, c.ID, "notes", "Notes saved.")
	default:
		http.Error(w, "unknown field", http.StatusBadRequest)
	}
}

func flushNotes(c *Component) string {
	var b []string
	for _, f := range c.FlushLog {
		b = append(b, f.Note)
	}
	return strings.Join(b, " ")
}

// weightText shows a weight to one decimal place at most, so adding
// 12.1 and 10.2 shows 22.3 rather than 22.299999999999997.
func weightText(f float64) string {
	if f == 0 {
		return ""
	}
	return strconv.FormatFloat(math.Round(f*10)/10, 'f', -1, 64)
}
