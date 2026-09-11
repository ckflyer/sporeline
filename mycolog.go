package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ============================================================
// A very small read-only SQLite reader.
//
// Sporeline stores its own data in a JSON file, so it has no database
// driver and no C compiler in the build. Reading someone else's SQLite
// file once, at import time, does not justify either. This handles what
// an import needs — walking a table and decoding its rows — and nothing
// else: no queries, no indexes, no writing.
// ============================================================

type sqliteFile struct {
	data     []byte
	pageSize int
	usable   int
}

type sqliteRow struct {
	rowID  int64
	values []any
}

func openSQLite(path string) (*sqliteFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 100 || string(data[:15]) != "SQLite format 3" {
		return nil, fmt.Errorf("%s is not a SQLite database", filepath.Base(path))
	}
	pageSize := int(binary.BigEndian.Uint16(data[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize&(pageSize-1) != 0 {
		return nil, fmt.Errorf("odd page size %d", pageSize)
	}
	return &sqliteFile{data: data, pageSize: pageSize, usable: pageSize - int(data[20])}, nil
}

func (f *sqliteFile) page(n int) ([]byte, error) {
	if n < 1 {
		return nil, fmt.Errorf("bad page number %d", n)
	}
	start := (n - 1) * f.pageSize
	if start+f.pageSize > len(f.data) {
		return nil, fmt.Errorf("page %d is past the end of the file", n)
	}
	return f.data[start : start+f.pageSize], nil
}

// varint reads SQLite's big-endian 7-bits-per-byte integer.
func varint(b []byte) (int64, int) {
	var v uint64
	for i := 0; i < 8 && i < len(b); i++ {
		v = v<<7 | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return int64(v), i + 1
		}
	}
	if len(b) < 9 {
		return 0, 0
	}
	return int64(v<<8 | uint64(b[8])), 9
}

// table walks a table b-tree from its root page and returns every row.
func (f *sqliteFile) table(root int) ([]sqliteRow, error) {
	var out []sqliteRow
	seen := map[int]bool{}
	var walk func(int) error
	walk = func(n int) error {
		if seen[n] {
			return nil // a cycle would mean a corrupt file; stop rather than spin
		}
		seen[n] = true
		page, err := f.page(n)
		if err != nil {
			return err
		}
		offset := 0
		if n == 1 {
			offset = 100 // page 1 carries the file header first
		}
		kind := page[offset]
		cells := int(binary.BigEndian.Uint16(page[offset+3 : offset+5]))
		headerLen := 8
		if kind == 0x05 || kind == 0x02 {
			headerLen = 12
		}
		pointers := page[offset+headerLen : offset+headerLen+cells*2]

		switch kind {
		case 0x05: // interior table: descend
			for i := 0; i < cells; i++ {
				at := int(binary.BigEndian.Uint16(pointers[i*2 : i*2+2]))
				child := int(binary.BigEndian.Uint32(page[at : at+4]))
				if err := walk(child); err != nil {
					return err
				}
			}
			right := int(binary.BigEndian.Uint32(page[offset+8 : offset+12]))
			return walk(right)
		case 0x0d: // leaf table: read the rows
			for i := 0; i < cells; i++ {
				at := int(binary.BigEndian.Uint16(pointers[i*2 : i*2+2]))
				if at <= 0 || at >= len(page) {
					continue
				}
				cell := page[at:]
				size, n1 := varint(cell)
				rowID, n2 := varint(cell[n1:])
				body := cell[n1+n2:]
				payload, err := f.payload(body, int(size))
				if err != nil {
					return err
				}
				values, err := decodeRecord(payload)
				if err != nil {
					return err
				}
				out = append(out, sqliteRow{rowID: rowID, values: values})
			}
			return nil
		default:
			return fmt.Errorf("unexpected b-tree page type %#x", kind)
		}
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// payload stitches a row back together when it spills onto overflow pages.
func (f *sqliteFile) payload(cell []byte, size int) ([]byte, error) {
	maxLocal := f.usable - 35
	if size <= maxLocal {
		if size > len(cell) {
			return nil, fmt.Errorf("row runs past the end of its page")
		}
		return cell[:size], nil
	}
	minLocal := ((f.usable-12)*32)/255 - 23
	local := minLocal + (size-minLocal)%(f.usable-4)
	if local > maxLocal {
		local = minLocal
	}
	if local+4 > len(cell) {
		return nil, fmt.Errorf("row runs past the end of its page")
	}
	out := make([]byte, 0, size)
	out = append(out, cell[:local]...)
	next := int(binary.BigEndian.Uint32(cell[local : local+4]))
	for next != 0 && len(out) < size {
		page, err := f.page(next)
		if err != nil {
			return nil, err
		}
		next = int(binary.BigEndian.Uint32(page[:4]))
		chunk := page[4:f.usable]
		if need := size - len(out); need < len(chunk) {
			chunk = chunk[:need]
		}
		out = append(out, chunk...)
	}
	if len(out) != size {
		return nil, fmt.Errorf("row is shorter than it claims")
	}
	return out, nil
}

// decodeRecord turns SQLite's record format into Go values.
func decodeRecord(rec []byte) ([]any, error) {
	headerLen, n := varint(rec)
	if n == 0 || int(headerLen) > len(rec) {
		return nil, fmt.Errorf("bad record header")
	}
	var types []int64
	for at := n; at < int(headerLen); {
		t, used := varint(rec[at:])
		if used == 0 {
			return nil, fmt.Errorf("bad record header")
		}
		types = append(types, t)
		at += used
	}
	body := rec[headerLen:]
	out := make([]any, 0, len(types))
	read := func(n int) []byte {
		if n > len(body) {
			n = len(body)
		}
		b := body[:n]
		body = body[n:]
		return b
	}
	signed := func(b []byte) int64 {
		var v int64
		for _, c := range b {
			v = v<<8 | int64(c)
		}
		bits := uint(len(b)) * 8
		if bits < 64 && v&(1<<(bits-1)) != 0 {
			v -= 1 << bits
		}
		return v
	}
	for _, t := range types {
		switch {
		case t == 0:
			out = append(out, nil)
		case t >= 1 && t <= 4:
			out = append(out, signed(read(int(t))))
		case t == 5:
			out = append(out, signed(read(6)))
		case t == 6:
			out = append(out, signed(read(8)))
		case t == 7:
			out = append(out, math.Float64frombits(binary.BigEndian.Uint64(read(8))))
		case t == 8:
			out = append(out, int64(0))
		case t == 9:
			out = append(out, int64(1))
		case t >= 12 && t%2 == 0:
			out = append(out, append([]byte(nil), read(int(t-12)/2)...))
		case t >= 13 && t%2 == 1:
			out = append(out, string(read(int(t-13)/2)))
		default:
			out = append(out, nil)
		}
	}
	return out, nil
}

// schema finds a table's root page and column names.
func (f *sqliteFile) schema() (map[string]int, map[string][]string, error) {
	rows, err := f.table(1)
	if err != nil {
		return nil, nil, err
	}
	roots := map[string]int{}
	cols := map[string][]string{}
	for _, r := range rows {
		if len(r.values) < 5 {
			continue
		}
		kind, _ := r.values[0].(string)
		name, _ := r.values[1].(string)
		if kind != "table" {
			continue
		}
		switch v := r.values[3].(type) {
		case int64:
			roots[name] = int(v)
		}
		if sql, ok := r.values[4].(string); ok {
			cols[name] = columnNames(sql)
		}
	}
	return roots, cols, nil
}

// columnNames pulls column names out of a CREATE TABLE statement.
func columnNames(sql string) []string {
	sql = stripSQLComments(sql)
	open := strings.Index(sql, "(")
	if open < 0 {
		return nil
	}
	body := sql[open+1:]
	if close := strings.LastIndex(body, ")"); close >= 0 {
		body = body[:close]
	}
	var parts []string
	depth, start := 0, 0
	for i, c := range body {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, body[start:])

	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name := strings.Fields(p)[0]
		if i := strings.IndexByte(name, '('); i > 0 {
			name = name[:i] // "CHECK(yield" is a constraint, not a column
		}
		switch strings.ToUpper(name) {
		case "PRIMARY", "FOREIGN", "UNIQUE", "CHECK", "CONSTRAINT":
			continue // a table constraint, not a column
		}
		out = append(out, strings.Trim(name, "`\"[]"))
	}
	return out
}

// stripSQLComments removes -- line comments and /* block */ comments so
// they cannot be mistaken for column definitions.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i++
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(sql[i])
	}
	return b.String()
}

// ============================================================
// The import itself
// ============================================================

type importReport struct {
	Cultures int
	Gone     int
	Pictures int
	Skipped  int
	Warning  string
}

func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func number(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

// pick maps a row onto its column names.
func pick(cols []string, r sqliteRow) map[string]any {
	m := map[string]any{}
	for i, name := range cols {
		if i < len(r.values) {
			m[name] = r.values[i]
		}
	}
	// An INTEGER PRIMARY KEY column is stored as NULL; its real value is
	// the row id.
	if v, ok := m["id"]; ok && v == nil {
		m["id"] = r.rowID
	}
	return m
}

var oldKinds = map[string]string{"SPORES": "spores", "MYC": "myc", "SPAWN": "spawn", "GROW": "grow"}

// legacyComp is one entry as the other program stored it.
type legacyComp struct {
	OldID   int64
	Token   string
	Kind    string
	Species string
	Created string
	Notes   string
	Gone    bool
	Yield   float64
	Remarks string
	Parents []int64
	Gen     int
}

// loadLegacy reads the whole of another program's log into memory.
func loadLegacy(path string) ([]legacyComp, string, string, error) {
	path = strings.TrimSpace(strings.Trim(strings.TrimSpace(path), `"'`))
	if path == "" {
		return nil, "", "", fmt.Errorf("give me the folder that holds the database")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("cannot find %s", path)
	}
	folder, dbPath := path, path
	if info.IsDir() {
		dbPath = filepath.Join(path, "mycolog.sqlite3")
		if _, err := os.Stat(dbPath); err != nil {
			matches, _ := filepath.Glob(filepath.Join(path, "*.sqlite3"))
			if len(matches) == 0 {
				return nil, "", "", fmt.Errorf("no .sqlite3 database in %s", path)
			}
			dbPath = matches[0]
		}
	} else {
		folder = filepath.Dir(path)
	}
	warning := ""
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		warning = "There is a -wal file next to the database, so the other program may still have unsaved changes. Close it and try again if anything looks missing."
	}

	f, err := openSQLite(dbPath)
	if err != nil {
		return nil, "", "", err
	}
	roots, cols, err := f.schema()
	if err != nil {
		return nil, "", "", err
	}
	if _, ok := roots["component"]; !ok {
		return nil, "", "", fmt.Errorf("that database has no component table, so it is not a cultivation log")
	}
	compRows, err := f.table(roots["component"])
	if err != nil {
		return nil, "", "", fmt.Errorf("could not read components: %w", err)
	}
	parents := map[int64][]int64{}
	if root, ok := roots["relation"]; ok {
		if relRows, err := f.table(root); err == nil {
			rc := cols["relation"]
			for _, r := range relRows {
				m := pick(rc, r)
				child, parent := number(m["child"]), number(m["parent"])
				parents[child] = append(parents[child], parent)
			}
		}
	}
	grows := map[int64][2]any{}
	if root, ok := roots["grow"]; ok {
		if growRows, err := f.table(root); err == nil {
			gc := cols["grow"]
			for _, r := range growRows {
				m := pick(gc, r)
				grows[number(m["id"])] = [2]any{m["yield"], m["yieldComment"]}
			}
		}
	}

	gen := map[int64]int{}
	var depth func(int64, map[int64]bool) int
	depth = func(id int64, seen map[int64]bool) int {
		if g, ok := gen[id]; ok {
			return g
		}
		if seen[id] {
			return 0
		}
		seen[id] = true
		best := -1
		for _, p := range parents[id] {
			if d := depth(p, seen); d > best {
				best = d
			}
		}
		gen[id] = best + 1
		return gen[id]
	}

	cc := cols["component"]
	sort.Slice(compRows, func(i, j int) bool { return compRows[i].rowID < compRows[j].rowID })
	var out []legacyComp
	for _, r := range compRows {
		m := pick(cc, r)
		oldID := number(m["id"])
		created := text(m["createdAt"])
		if len(created) > 10 {
			created = created[:10]
		}
		lc := legacyComp{
			OldID:   oldID,
			Token:   strings.ToUpper(strings.TrimSpace(text(m["token"]))),
			Kind:    oldKinds[strings.ToUpper(text(m["type"]))],
			Species: text(m["species"]),
			Created: created,
			Notes:   text(m["notes"]),
			Gone:    number(m["gone"]) != 0,
			Parents: parents[oldID],
			Gen:     depth(oldID, map[int64]bool{}),
		}
		if lc.Kind == "" {
			lc.Kind = "myc"
		}
		if g, ok := grows[oldID]; ok {
			if mg := number(g[0]); mg > 0 {
				lc.Yield = float64(mg) / 1000
			}
			lc.Remarks = text(g[1])
		}
		out = append(out, lc)
	}
	return out, folder, warning, nil
}

// ImportLegacy brings another program's log in as new entries, keeping
// the IDs exactly as they were.
func ImportLegacy(path string) (importReport, error) {
	var rep importReport
	list, folder, warning, err := loadLegacy(path)
	if err != nil {
		return rep, err
	}
	rep.Warning = warning

	taken := map[string]bool{}
	for _, c := range store.All() {
		taken[c.ID] = true
	}
	idMap := map[int64]string{}
	var staged []struct {
		old int64
		c   *Component
	}
	for _, lc := range list {
		id := lc.Token
		if id == "" {
			id = fmt.Sprintf("X%d", lc.OldID)
		}
		if taken[id] {
			rep.Skipped++
			idMap[lc.OldID] = id
			continue
		}
		taken[id] = true
		idMap[lc.OldID] = id
		comp := &Component{
			ID: id, Kind: lc.Kind, Species: lc.Species, Created: lc.Created,
			Notes: lc.Notes, Gone: lc.Gone, Gen: lc.Gen, Yield: lc.Yield, Remarks: lc.Remarks,
		}
		if comp.Species == "" {
			comp.Species = "Unnamed"
		}
		if comp.Gone {
			comp.GoneReason = "unknown"
			rep.Gone++
		}
		staged = append(staged, struct {
			old int64
			c   *Component
		}{lc.OldID, comp})
	}
	byOld := map[int64]*Component{}
	for _, s := range staged {
		byOld[s.old] = s.c
	}
	for _, s := range staged {
		for _, p := range parentsOf(list, s.old) {
			if mapped, ok := idMap[p]; ok {
				s.c.Parents = append(s.c.Parents, mapped)
			}
		}
	}
	rep.Pictures = copyLegacyPics(folder, byOld)
	for _, s := range staged {
		if err := store.AddImported(s.c); err != nil {
			return rep, err
		}
		rep.Cultures++
	}
	return rep, store.Save()
}

func parentsOf(list []legacyComp, old int64) []int64 {
	for _, lc := range list {
		if lc.OldID == old {
			return lc.Parents
		}
	}
	return nil
}

// copyLegacyPics copies picture files across, renaming them to match the
// entry they belong to.
func copyLegacyPics(folder string, byOld map[int64]*Component) int {
	picsIn := filepath.Join(folder, "pics")
	entries, err := os.ReadDir(picsIn)
	if err != nil {
		return 0
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	n := 0
	for _, name := range names {
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		idPart, posPart, _ := strings.Cut(stem, "_")
		oldID, err := strconv.ParseInt(idPart, 10, 64)
		if err != nil {
			continue
		}
		comp, ok := byOld[oldID]
		if !ok {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(picsIn, name))
		if err != nil {
			continue
		}
		if posPart == "" {
			posPart = "0"
		}
		newName := fmt.Sprintf("%s_%s.%s", comp.ID, posPart, ext)
		if err := os.WriteFile(filepath.Join(store.PicsDir(), newName), raw, 0o600); err != nil {
			continue
		}
		comp.Pics = append(comp.Pics, newName)
		n++
	}
	return n
}
