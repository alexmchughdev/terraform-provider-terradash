package dashboard

import (
	"encoding/json"
	"maps"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
)

// Decode converts a Grafana dashboard model into a tree matching Body.
// Anything that has no typed home is kept verbatim in extra.
func Decode(model map[string]any) map[string]any {
	rest := maps.Clone(model)
	delete(rest, "id")
	delete(rest, "version")
	if rest["uid"] == "" {
		delete(rest, "uid")
	}

	d := decodeFields(dashboardFields, rest)
	if panels, ok := decodePanels(rest["panels"]); ok {
		delete(rest, "panels")
		setPanels(d, panels)
	}
	if vars, ok := decodeList(rest["templating"], variableFields); ok {
		delete(rest, "templating")
		d["variable"] = vars
	}
	if annotations, ok := decodeList(rest["annotations"], annotationFields); ok {
		delete(rest, "annotations")
		d["annotation"] = annotations
	}
	if links, ok := decodeElements(rest["links"], linkFields); ok {
		delete(rest, "links")
		d["link"] = links
	}
	d["extra"] = extra(rest)
	return d
}

func setPanels(d map[string]any, panels []placed) {
	var top, rows []any
	for _, p := range panels {
		if p.model["type"] != "row" {
			top = append(top, p.tree)
			continue
		}
		var children []any
		for _, c := range p.children {
			children = append(children, c.tree)
		}
		if len(children) > 0 {
			p.tree["panel"] = children
		}
		rows = append(rows, p.tree)
	}
	if len(top) > 0 {
		d["panel"] = top
	}
	if len(rows) > 0 {
		d["row"] = rows
	}
}

func extra(rest map[string]any) any {
	if len(rest) == 0 {
		return nil
	}
	return rest
}

func decodeFields(fields []field, rest map[string]any) map[string]any {
	d := map[string]any{}
	for _, f := range fields {
		v, ok := rest[f.key]
		if !ok {
			continue
		}
		if v == nil || (v == "" && !f.required) {
			delete(rest, f.key)
			continue
		}
		if tv, ok := decodeValue(f.kind, f.sub, v); ok {
			d[f.name] = tv
			delete(rest, f.key)
		}
	}
	return d
}

func decodeValue(kind schema.Kind, sub []schema.Attribute, v any) (any, bool) {
	switch kind {
	case schema.String:
		_, ok := v.(string)
		return v, ok
	case schema.Number:
		_, ok := v.(json.Number)
		return v, ok
	case schema.Bool:
		_, ok := v.(bool)
		return v, ok
	case schema.StringList:
		return v, isStringList(v)
	case schema.Object:
		return decodeObject(sub, v)
	}
	return v, true
}

func isStringList(v any) bool {
	items, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

func decodeObject(attrs []schema.Attribute, v any) (any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	out := map[string]any{}
	for _, a := range attrs {
		e, ok := m[a.Name]
		if !ok || e == nil {
			if a.Required {
				return nil, false
			}
			continue
		}
		if _, ok := decodeValue(a.Kind, nil, e); !ok {
			return nil, false
		}
		out[a.Name] = e
	}
	if len(out) != len(m) {
		return nil, false
	}
	return out, true
}

func decodeElement(fields []field, v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	rest := maps.Clone(m)
	d := decodeFields(fields, rest)
	for _, f := range fields {
		if f.required && d[f.name] == nil {
			return nil, false
		}
	}
	d["extra"] = extra(rest)
	return d, true
}

func decodeElements(v any, fields []field) ([]any, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		d, ok := decodeElement(fields, item)
		if !ok {
			return nil, false
		}
		out = append(out, d)
	}
	return out, true
}

func decodeList(v any, fields []field) ([]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return nil, false
	}
	return decodeElements(m["list"], fields)
}

// decodePanels splits Grafana's flat panel list into top-level panels and rows.
// Panels following an expanded row belong to it, as in the Grafana UI.
func decodePanels(v any) ([]placed, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	var out []placed
	var row *placed
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		if m["type"] == "row" {
			p, ok := decodeRow(m)
			if !ok {
				return nil, false
			}
			out = append(out, p)
			row = &out[len(out)-1]
			continue
		}
		p, ok := decodePanel(m)
		if !ok {
			return nil, false
		}
		switch {
		case row == nil:
			out = append(out, p)
		case isCollapsed(row.tree):
			return nil, false
		default:
			row.children = append(row.children, p)
		}
	}
	return out, true
}

func decodePanel(m map[string]any) (placed, bool) {
	d, ok := decodeElement(panelFields, m)
	if !ok || d["type"] == "row" {
		return placed{}, false
	}
	return placed{tree: d, model: m}, true
}

func decodeRow(m map[string]any) (placed, bool) {
	rest := maps.Clone(m)
	delete(rest, "type")
	nested, isList := rest["panels"].([]any)
	if rest["panels"] != nil && !isList {
		return placed{}, false
	}
	delete(rest, "panels")

	d, ok := decodeElement(rowFields, rest)
	if !ok {
		return placed{}, false
	}
	if !isCollapsed(d) {
		return placed{tree: d, model: m}, len(nested) == 0
	}
	p := placed{tree: d, model: m}
	for _, item := range nested {
		c, ok := item.(map[string]any)
		if !ok {
			return placed{}, false
		}
		child, ok := decodePanel(c)
		if !ok {
			return placed{}, false
		}
		p.children = append(p.children, child)
	}
	return p, true
}
