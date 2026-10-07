package dashboard

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
)

type Diagnostic struct {
	Path    []any
	Summary string
	Warning bool
}

var (
	uidPattern      = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	variablePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	refreshPattern  = regexp.MustCompile(`^(auto|\d+(ms|s|m|h|d|w|M|y))?$`)
	variableTypes   = []string{"query", "custom", "constant", "datasource", "interval", "textbox", "adhoc", "groupby", "switch"}
)

const UIDRule = "must be 1-40 characters of letters, digits, '-' or '_'"

func ValidUID(s string) bool {
	return uidPattern.MatchString(s)
}

// Validate checks a dashboard tree, skipping values that are not yet known.
func Validate(d map[string]any) []Diagnostic {
	v := &validator{ids: map[string]bool{}}
	v.dashboard(d)
	return v.diags
}

type validator struct {
	diags []Diagnostic
	ids   map[string]bool
}

func (v *validator) errorf(path attrPath, format string, args ...any) {
	v.diags = append(v.diags, Diagnostic{Path: path, Summary: fmt.Sprintf(format, args...)})
}

func (v *validator) warnf(path attrPath, format string, args ...any) {
	v.diags = append(v.diags, Diagnostic{Path: path, Summary: fmt.Sprintf(format, args...), Warning: true})
}

func (v *validator) dashboard(d map[string]any) {
	if s, ok := d["uid"].(string); ok && !ValidUID(s) {
		v.errorf(path("uid"), "uid "+UIDRule)
	}
	if s, ok := d["refresh"].(string); ok && !refreshPattern.MatchString(s) {
		v.errorf(path("refresh"), "refresh must be an interval such as 30s, 5m or 1h")
	}
	v.intRange(d, path(), "graph_tooltip", 0, 2)
	v.intRange(d, path(), "fiscal_year_start_month", 0, 11)
	v.intRange(d, path(), "schema_version", 0, 1000)

	v.extra(d, path(), dashboardFields, "panels", "templating", "annotations", "links")
	for i, p := range blockList(d["panel"]) {
		v.panel(p, path("panel", i))
	}
	for i, r := range blockList(d["row"]) {
		v.row(r, path("row", i))
	}
	v.variables(blockList(d["variable"]))
	for i, a := range blockList(d["annotation"]) {
		v.extra(a, path("annotation", i), annotationFields)
	}
	for i, l := range blockList(d["link"]) {
		v.extra(l, path("link", i), linkFields)
	}
}

func (v *validator) panel(p map[string]any, at attrPath) {
	if p["type"] == "row" {
		v.errorf(at.with("type"), `use a row block instead of a panel with type "row"`)
	}
	if p["type"] == nil && p["library_panel"] == nil {
		v.errorf(at, "type is required unless library_panel is set")
	}
	v.id(p, at)
	v.gridPos(p, at)
	v.dynamicKind(p, at, "datasource", "object", "string")
	v.dynamicKind(p, at, "targets", "list")
	v.dynamicKind(p, at, "options", "object")
	v.dynamicKind(p, at, "field_config", "object")
	v.dynamicKind(p, at, "transformations", "list")
	v.dynamicKind(p, at, "links", "list")
	v.dynamicKind(p, at, "library_panel", "object")
	v.intRange(p, at, "max_per_row", 1, 100)
	v.refIDs(p, at)
	v.extra(p, at, panelFields)
}

func (v *validator) row(r map[string]any, at attrPath) {
	v.id(r, at)
	v.gridPos(r, at)
	v.dynamicKind(r, at, "datasource", "object", "string")
	v.extra(r, at, rowFields, "type", "panels")
	for i, p := range blockList(r["panel"]) {
		v.panel(p, at.with("panel", i))
	}
}

func (v *validator) variables(vars []map[string]any) {
	names := map[string]bool{}
	for i, vr := range vars {
		at := path("variable", i)
		if name, ok := vr["name"].(string); ok {
			if !variablePattern.MatchString(name) {
				v.errorf(at.with("name"), "variable names may only contain letters, digits and '_'")
			}
			if names[name] {
				v.errorf(at.with("name"), "duplicate variable %q", name)
			}
			names[name] = true
		}
		if t, ok := vr["type"].(string); ok && !slices.Contains(variableTypes, t) {
			v.warnf(at.with("type"), "unrecognised variable type %q", t)
		}
		v.intRange(vr, at, "hide", 0, 2)
		v.dynamicKind(vr, at, "datasource", "object", "string")
		v.dynamicKind(vr, at, "options", "list")
		v.extra(vr, at, variableFields)
	}
}

func (v *validator) id(m map[string]any, at attrPath) {
	n, ok := m["id"].(json.Number)
	if !ok {
		return
	}
	id, err := strconv.Atoi(string(n))
	if err != nil || id < 0 {
		v.errorf(at.with("id"), "id must be a non-negative integer")
		return
	}
	if id > 0 && v.ids[string(n)] {
		v.errorf(at.with("id"), "duplicate panel id %s", n)
	}
	v.ids[string(n)] = true
}

func (v *validator) gridPos(m map[string]any, at attrPath) {
	pos, ok := m["grid_pos"].(map[string]any)
	if !ok {
		return
	}
	at = at.with("grid_pos")
	v.intRange(pos, at, "x", 0, gridColumns-1)
	v.intRange(pos, at, "y", 0, 1<<20)
	v.intRange(pos, at, "w", 1, gridColumns)
	v.intRange(pos, at, "h", 1, 1<<20)
	x, hasX := intAt(pos, "x")
	w, hasW := intAt(pos, "w")
	if hasX && hasW && x+w > gridColumns {
		v.errorf(at, "x + w must not exceed %d", gridColumns)
	}
}

func (v *validator) refIDs(p map[string]any, at attrPath) {
	targets, _ := p["targets"].([]any)
	seen := map[string]bool{}
	for i, t := range targets {
		m, _ := t.(map[string]any)
		ref, ok := m["refId"].(string)
		if !ok {
			continue
		}
		if seen[ref] {
			v.warnf(at.with("targets", i), "duplicate refId %q", ref)
		}
		seen[ref] = true
	}
}

func (v *validator) intRange(m map[string]any, at attrPath, key string, lo, hi int) {
	n, ok := m[key].(json.Number)
	if !ok {
		return
	}
	i, err := strconv.Atoi(string(n))
	if err != nil || i < lo || i > hi {
		v.errorf(at.with(key), "%s must be an integer between %d and %d", key, lo, hi)
	}
}

func (v *validator) dynamicKind(m map[string]any, at attrPath, key string, kinds ...string) {
	val := m[key]
	if val == nil || schema.HasUnknown(val) {
		return
	}
	if !slices.Contains(kinds, kindOf(val)) {
		v.errorf(at.with(key), "%s must be of type %s", key, strings.Join(kinds, " or "))
	}
}

func (v *validator) extra(m map[string]any, at attrPath, fields []field, reserved ...string) {
	val := m["extra"]
	if val == nil || schema.IsUnknown(val) {
		return
	}
	extra, ok := val.(map[string]any)
	if !ok {
		v.errorf(at.with("extra"), "extra must be an object")
		return
	}
	for _, f := range fields {
		if _, clash := extra[f.key]; clash && m[f.name] != nil {
			v.errorf(at.with("extra"), "extra key %q conflicts with argument %q", f.key, f.name)
		}
	}
	for _, key := range reserved {
		if _, clash := extra[key]; clash && reservedInUse(m, key) {
			v.errorf(at.with("extra"), "extra key %q conflicts with nested blocks", key)
		}
	}
}

func reservedInUse(m map[string]any, key string) bool {
	switch key {
	case "panels":
		return len(blockList(m["panel"])) > 0 || len(blockList(m["row"])) > 0
	case "templating":
		return len(blockList(m["variable"])) > 0
	case "annotations":
		return len(blockList(m["annotation"])) > 0
	case "links":
		return len(blockList(m["link"])) > 0
	}
	return true
}

func kindOf(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "list"
	case string:
		return "string"
	case json.Number:
		return "number"
	case bool:
		return "bool"
	}
	return "unknown"
}

type attrPath []any

func path(steps ...any) attrPath { return steps }

func (p attrPath) with(steps ...any) attrPath {
	return append(slices.Clone(p), steps...)
}
