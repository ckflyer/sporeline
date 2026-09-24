package main

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Close lineage: start at this component's parents (or itself if it has
// none) and take everything below. That is you, your siblings and all
// your descendants — the same rule mycolog uses.
func (s *Store) CloseLineage(id string) []*Component {
	roots := []string{}
	for _, p := range s.Parents(id) {
		roots = append(roots, p.ID)
	}
	if len(roots) == 0 {
		roots = []string{id}
	}
	return s.descendFrom(roots)
}

// Full lineage: climb to the genetic roots first, then take everything
// below them.
func (s *Store) FullLineage(id string) []*Component {
	visited := map[string]bool{}
	var roots []string
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		ps := s.Parents(cur)
		if len(ps) == 0 {
			roots = append(roots, cur)
			continue
		}
		for _, p := range ps {
			queue = append(queue, p.ID)
		}
	}
	return s.descendFrom(roots)
}

func (s *Store) descendFrom(roots []string) []*Component {
	visited := map[string]bool{}
	var out []*Component
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		c := s.Get(cur)
		if c == nil {
			continue
		}
		out = append(out, c)
		for _, ch := range s.Children(cur) {
			queue = append(queue, ch.ID)
		}
	}
	return out
}

// ---------- drawing ----------

const (
	nodeW = 132
	nodeH = 46
	gapX  = 18
	gapY  = 58
	padX  = 16
	padY  = 30
)

type placed struct {
	c    *Component
	x, y int
	row  int
}

// FamilySVG lays the family out in rows by generation and draws it.
// No Graphviz, no external anything — just arithmetic and an SVG string.
func (s *Store) FamilySVG(family []*Component, currentID string) string {
	if len(family) == 0 {
		return ""
	}
	in := map[string]bool{}
	for _, c := range family {
		in[c.ID] = true
	}

	// Row = depth below the shallowest generation on screen, so a
	// close-lineage view starting at generation 4 still begins at row 0.
	minGen := family[0].Gen
	for _, c := range family {
		if c.Gen < minGen {
			minGen = c.Gen
		}
	}
	rows := map[int][]*Component{}
	maxRow := 0
	for _, c := range family {
		r := c.Gen - minGen
		rows[r] = append(rows[r], c)
		if r > maxRow {
			maxRow = r
		}
	}
	for r := range rows {
		sort.SliceStable(rows[r], func(i, j int) bool {
			return rows[r][i].Created < rows[r][j].Created
		})
	}

	// Order each row so children sit near their parents.
	pos := map[string]int{}
	reindex := func() {
		for r := 0; r <= maxRow; r++ {
			for i, c := range rows[r] {
				pos[c.ID] = i
			}
		}
	}
	reindex()
	for pass := 0; pass < 4; pass++ {
		down := pass%2 == 0
		for step := 0; step <= maxRow; step++ {
			r := step
			if !down {
				r = maxRow - step
			}
			list := rows[r]
			if len(list) < 2 {
				continue
			}
			weight := map[string]float64{}
			for _, c := range list {
				var vals []float64
				if down {
					for _, p := range c.Parents {
						if in[p] {
							vals = append(vals, float64(pos[p]))
						}
					}
				} else {
					for _, ch := range s.Children(c.ID) {
						if in[ch.ID] {
							vals = append(vals, float64(pos[ch.ID]))
						}
					}
				}
				if len(vals) == 0 {
					weight[c.ID] = float64(pos[c.ID])
					continue
				}
				sum := 0.0
				for _, v := range vals {
					sum += v
				}
				weight[c.ID] = sum / float64(len(vals))
			}
			sort.SliceStable(list, func(i, j int) bool {
				return weight[list[i].ID] < weight[list[j].ID]
			})
			reindex()
		}
	}

	widest := 1
	for r := 0; r <= maxRow; r++ {
		if len(rows[r]) > widest {
			widest = len(rows[r])
		}
	}
	boardW := widest*(nodeW+gapX) - gapX
	at := map[string]placed{}
	for r := 0; r <= maxRow; r++ {
		rowW := len(rows[r])*(nodeW+gapX) - gapX
		x0 := padX + (boardW-rowW)/2
		for i, c := range rows[r] {
			at[c.ID] = placed{c: c, x: x0 + i*(nodeW+gapX), y: padY + r*(nodeH+gapY), row: r}
		}
	}
	width := boardW + padX*2
	height := padY*2 + (maxRow+1)*(nodeH+gapY) - gapY

	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="family" viewBox="0 0 %d %d" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">`,
		width, height, width, height)

	// generation rules
	for r := 0; r <= maxRow; r++ {
		top := padY + r*(nodeH+gapY)
		fmt.Fprintf(&b, `<line x1="0" y1="%d" x2="%d" y2="%d" stroke="#7C6553" stroke-dasharray="2 6"/>`, top-11, width, top-11)
		fmt.Fprintf(&b, `<text x="2" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="9" fill="#CDBBA3">G%d</text>`,
			top-15, minGen+r)
	}
	// edges
	for _, c := range family {
		cp := at[c.ID]
		for _, pid := range c.Parents {
			pp, ok := at[pid]
			if !ok {
				continue
			}
			x1, y1 := pp.x+nodeW/2, pp.y+nodeH
			x2, y2 := cp.x+nodeW/2, cp.y
			fmt.Fprintf(&b, `<path d="M%d %d C %d %d, %d %d, %d %d" fill="none" stroke="#D9C7AB" stroke-width="1.6"/>`,
				x1, y1, x1, y1+26, x2, y2-26, x2, y2)
		}
	}
	// nodes
	for _, c := range family {
		p := at[c.ID]
		k := KindOf(c.Kind)
		stroke, sw := "#C7CEB8", "1"
		if c.ID == currentID {
			stroke, sw = "#E9C46A", "3" // a warm ring that reads on the brown
		}
		op := "1"
		if c.Gone {
			op = "0.55"
		}
		label := c.Title()
		if c.Sub != "" {
			label = label + " · " + c.Sub
		}
		fmt.Fprintf(&b, `<a href="/component/%s"><g opacity="%s">`, html.EscapeString(c.ID), op)
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="6" fill="#FBFCF5" stroke="%s" stroke-width="%s"/>`,
			p.x, p.y, nodeW, nodeH, stroke, sw)
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="4" height="%d" rx="2" fill="%s"/>`, p.x, p.y, nodeH, k.Color)
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="12" fill="#16211A">%s</text>`,
			p.x+12, p.y+19, html.EscapeString(c.ID))
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="ui-sans-serif,system-ui,sans-serif" font-size="10.5" fill="#63715F">%s</text>`,
			p.x+12, p.y+34, html.EscapeString(clip(label, 22)))
		if c.Gone {
			fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="8" fill="#9A3A2C" text-anchor="end">GONE</text>`,
				p.x+nodeW-8, p.y+15)
		}
		b.WriteString(`</g></a>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// AllRelatives walks parent and child links in both directions and
// returns everything connected to this component. Renaming a species
// uses it: when you finally identify a wild collection, everything that
// came out of it gets the new name too.
func (s *Store) AllRelatives(id string) []*Component {
	visited := map[string]bool{}
	var out []*Component
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		c := s.Get(cur)
		if c == nil {
			continue
		}
		out = append(out, c)
		for _, p := range c.Parents {
			queue = append(queue, p)
		}
		for _, ch := range s.Children(cur) {
			queue = append(queue, ch.ID)
		}
	}
	return out
}
