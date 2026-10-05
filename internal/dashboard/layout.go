package dashboard

import (
	"encoding/json"
	"strconv"
)

const (
	gridColumns   = 24
	defaultWidth  = 12
	defaultHeight = 8
)

// placed pairs a panel or row tree with its Grafana JSON model.
type placed struct {
	tree     map[string]any
	model    map[string]any
	children []placed
}

func flatten(panels []placed) []any {
	var out []any
	for _, p := range panels {
		out = append(out, p.model)
		if !isCollapsed(p.tree) {
			for _, c := range p.children {
				out = append(out, c.model)
			}
		}
	}
	return out
}

func isCollapsed(row map[string]any) bool {
	c, _ := row["collapsed"].(bool)
	return c
}

func layout(d map[string]any) ([]placed, error) {
	ids := newIDAllocator(d)
	g := &grid{}
	var out []placed
	for _, p := range blockList(d["panel"]) {
		pl, err := placePanel(p, g, ids)
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	for _, r := range blockList(d["row"]) {
		pl, err := placeRow(r, g, ids)
		if err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, nil
}

func placePanel(p map[string]any, g *grid, ids *idAllocator) (placed, error) {
	j, err := encodeElement(panelFields, p)
	if err != nil {
		return placed{}, err
	}
	ids.assign(j)
	if _, ok := j["gridPos"]; !ok || p["grid_pos"] != nil {
		j["gridPos"] = g.place(gridPos(p))
	}
	return placed{tree: p, model: j}, nil
}

func placeRow(r map[string]any, g *grid, ids *idAllocator) (placed, error) {
	j, err := encodeElement(rowFields, r)
	if err != nil {
		return placed{}, err
	}
	if _, ok := j["type"]; ok {
		return placed{}, errExtraKey("type")
	}
	j["type"] = "row"
	ids.assign(j)
	rowY := g.bottom
	if _, ok := j["gridPos"]; !ok || r["grid_pos"] != nil {
		rowY = g.rowY(gridPos(r))
		j["gridPos"] = g.row(rowY, gridPos(r))
	}

	pl := placed{tree: r, model: j}
	var nested []any
	for _, p := range blockList(r["panel"]) {
		c, err := placePanel(p, g, ids)
		if err != nil {
			return placed{}, err
		}
		pl.children = append(pl.children, c)
		nested = append(nested, c.model)
	}
	if isCollapsed(r) {
		if _, ok := j["panels"]; ok {
			return placed{}, errExtraKey("panels")
		}
		j["panels"] = append([]any{}, nested...)
		g.restart(rowY + 1)
	}
	return pl, nil
}

func gridPos(m map[string]any) map[string]any {
	pos, _ := m["grid_pos"].(map[string]any)
	return pos
}

type grid struct {
	x, y, bottom int
}

func (g *grid) place(pos map[string]any) map[string]any {
	w := intOr(pos, "w", defaultWidth)
	h := intOr(pos, "h", defaultHeight)
	x, hasX := intAt(pos, "x")
	y, hasY := intAt(pos, "y")
	if !hasX {
		x = g.x
		if x+w > gridColumns {
			x = 0
		}
	}
	if !hasY {
		y = g.y
		if x < g.x || x+w > gridColumns {
			y = g.bottom
		}
	}
	g.x, g.y = x+w, y
	g.bottom = max(g.bottom, y+h)
	return map[string]any{"x": x, "y": y, "w": w, "h": h}
}

func (g *grid) rowY(pos map[string]any) int {
	if y, ok := intAt(pos, "y"); ok {
		return y
	}
	return g.bottom
}

func (g *grid) row(y int, pos map[string]any) map[string]any {
	g.restart(y + 1)
	return map[string]any{"x": intOr(pos, "x", 0), "y": y, "w": intOr(pos, "w", gridColumns), "h": intOr(pos, "h", 1)}
}

func (g *grid) restart(y int) {
	g.x, g.y, g.bottom = 0, y, y
}

func intAt(m map[string]any, key string) (int, bool) {
	n, ok := m[key].(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(string(n))
	return i, err == nil
}

func intOr(m map[string]any, key string, def int) int {
	if i, ok := intAt(m, key); ok {
		return i
	}
	return def
}

type idAllocator struct {
	next int
}

func newIDAllocator(d map[string]any) *idAllocator {
	highest := 0
	for _, p := range allPanels(d) {
		if id, ok := intAt(p, "id"); ok {
			highest = max(highest, id)
		}
	}
	return &idAllocator{next: highest + 1}
}

func (a *idAllocator) assign(j map[string]any) {
	if _, ok := j["id"]; ok {
		return
	}
	j["id"] = a.next
	a.next++
}

// allPanels returns every panel and row tree in render order.
func allPanels(d map[string]any) []map[string]any {
	out := blockList(d["panel"])
	for _, r := range blockList(d["row"]) {
		out = append(out, r)
		out = append(out, blockList(r["panel"])...)
	}
	return out
}
