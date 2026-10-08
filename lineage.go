package main

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// closeLimit keeps the default tree readable. Past this many cards the
// view falls back to the smaller "parents down" family.
const closeLimit = 40

// CloseLineage is the tree shown by default: the whole line back to where
// it started, plus everything that came from the grandparents. That is
// this one, its siblings, its cousins, its children and theirs. The line
// above the grandparents is drawn without their other branches; "whole
// tree" adds those.
func (s *Store) CloseLineage(id string) []*Component {
	// Every ancestor, with how many steps up it is.
	up := map[string]int{id: 0}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, p := range s.Parents(cur) {
			if _, seen := up[p.ID]; !seen {
				up[p.ID] = up[cur] + 1
				queue = append(queue, p.ID)
			}
		}
	}
	from := func(steps int) []*Component {
		roots := []string{}
		for a, d := range up {
			if d == steps {
				roots = append(roots, a)
			}
		}
		sort.Strings(roots) // same picture every time
		if len(roots) == 0 {
			return nil
		}
		set := s.descendFrom(roots)
		have := map[string]bool{}
		for _, c := range set {
			have[c.ID] = true
		}
		for a := range up {
			if !have[a] {
				if c := s.Get(a); c != nil {
					set = append(set, c)
				}
			}
		}
		return set
	}
	for _, steps := range []int{2, 1} {
		if fam := from(steps); fam != nil && len(fam) <= closeLimit {
			return fam
		}
	}
	if fam := from(1); fam != nil {
		return fam // very large family: parents down, still with the line
	}
	return from(0)
}

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
	gapX  = 46 // room between side-by-side branches
	gapY  = 58
	padX  = 16
	padY  = 30
	// labelW is a left margin for the "GEN 3" labels, so they never sit
	// underneath a card.
	labelW  = 58
	stripeW = 7 // the coloured edge that says what kind a card is
	radius  = 6
	// minScale is how far the tree shrinks to fit its box before it
	// scrolls sideways instead.
	minScale = 0.6
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
		x0 := labelW + padX + (boardW-rowW)/2
		for i, c := range rows[r] {
			at[c.ID] = placed{c: c, x: x0 + i*(nodeW+gapX), y: padY + r*(nodeH+gapY), row: r}
		}
	}
	width := labelW + boardW + padX*2
	height := padY*2 + (maxRow+1)*(nodeH+gapY) - gapY

	var b strings.Builder
	// It shrinks to fit the box, down to minScale; only a tree wider than
	// that scrolls. The style carries the limits because they depend on
	// this tree's own size.
	fmt.Fprintf(&b, `<svg class="family" viewBox="0 0 %d %d" width="%d" height="%d" style="max-width:%dpx;min-width:%dpx" xmlns="http://www.w3.org/2000/svg">`,
		width, height, width, height, width, int(float64(width)*minScale))

	// generation rules
	for r := 0; r <= maxRow; r++ {
		top := padY + r*(nodeH+gapY)
		mid := top + nodeH/2
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#6E5747" stroke-dasharray="2 6"/>`, labelW, mid, width, mid)
		fmt.Fprintf(&b, `<text x="6" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="10.5" letter-spacing="1" fill="#E3D5BF">GEN %d</text>`,
			mid+4, minGen+r)
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
		this := c.ID == currentID
		op := "1"
		if c.Gone {
			op = "0.55"
		}
		label := c.Title()
		if c.Sub != "" {
			label = label + " · " + c.Sub
		}
		cls := ""
		if this {
			cls = ` class="this"`
		}
		fmt.Fprintf(&b, `<a href="/component/%s"%s>`, html.EscapeString(c.ID), cls)
		if this {
			// Drawn outside the card, so it never covers the coloured edge:
			// a soft glow, then a solid ring.
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="none" stroke="#E9C46A" stroke-opacity=".35" stroke-width="9"/>`,
				p.x-5, p.y-5, nodeW+10, nodeH+10, radius+5)
			fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="none" stroke="#F2C94C" stroke-width="3.5"/>`,
				p.x-4, p.y-4, nodeW+8, nodeH+8, radius+4)
		}
		fmt.Fprintf(&b, `<g opacity="%s">`, op)
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="#FBFCF5" stroke="#C7CEB8" stroke-width="1"/>`,
			p.x, p.y, nodeW, nodeH, radius)
		// The kind's colour fills the card's whole left edge, rounded to
		// match its corners, and covers the border there.
		x0, y0, y1 := float64(p.x)-0.5, float64(p.y)-0.5, float64(p.y+nodeH)+0.5
		r := float64(radius) + 0.5
		fmt.Fprintf(&b, `<path d="M%.1f %.1f H%.1f V%.1f H%.1f A%.1f %.1f 0 0 1 %.1f %.1f V%.1f A%.1f %.1f 0 0 1 %.1f %.1fZ" fill="%s"/>`,
			x0+r, y0, x0+stripeW+0.5, y1, x0+r, r, r, x0, y1-r, y0+r, r, r, x0+r, y0, k.Color)
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="ui-monospace,Menlo,Consolas,monospace" font-size="12" fill="#16211A">%s</text>`,
			p.x+stripeW+9, p.y+19, html.EscapeString(c.ID))
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="ui-sans-serif,system-ui,sans-serif" font-size="10.5" fill="#63715F">%s</text>`,
			p.x+stripeW+9, p.y+34, html.EscapeString(clip(label, 21)))
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
