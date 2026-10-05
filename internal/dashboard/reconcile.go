package dashboard

import (
	"fmt"
	"maps"
	"strconv"
)

// Reconcile decodes a remote model, keeping any prior value that renders to
// the same JSON so that drift only reports what actually changed.
func Reconcile(prior, model map[string]any) map[string]any {
	priorJSON, err := Encode(prior)
	if err != nil {
		return Decode(model)
	}
	remote := withoutVolatile(model)
	if Equal(priorJSON, remote) {
		return prior
	}

	d := Decode(model)
	keepFields(dashboardFields, prior, priorJSON, d, remote)
	reconcilePanels(prior, d, remote)
	reconcileList(variableFields, byName, prior, priorJSON, d, remote, "variable", "templating")
	reconcileList(annotationFields, byName, prior, priorJSON, d, remote, "annotation", "annotations")
	reconcileList(linkFields, byIndex, prior, priorJSON, d, remote, "link", "links")
	return d
}

// Matches reports whether a tree renders to the given remote model.
func Matches(tree, model map[string]any) bool {
	j, err := Encode(tree)
	return err == nil && Equal(j, withoutVolatile(model))
}

func withoutVolatile(model map[string]any) map[string]any {
	out := maps.Clone(model)
	delete(out, "id")
	delete(out, "version")
	return out
}

func keepFields(fields []field, prior, priorJSON, cur, remote map[string]any) {
	for _, f := range fields {
		if Equal(priorJSON[f.key], remote[f.key]) {
			cur[f.name] = prior[f.name]
		}
	}
	if Equal(prior["extra"], cur["extra"]) {
		cur["extra"] = prior["extra"]
	}
}

func reconcileElement(fields []field, prior, priorJSON, cur, remote map[string]any) map[string]any {
	if Equal(priorJSON, remote) {
		return prior
	}
	keepFields(fields, prior, priorJSON, cur, remote)
	return cur
}

type keyFunc func(i int, m map[string]any) string

func byName(_ int, m map[string]any) string { return fmt.Sprint(m["name"]) }

func byIndex(i int, _ map[string]any) string { return strconv.Itoa(i) }

func reconcileList(fields []field, key keyFunc, prior, priorJSON, d, remote map[string]any, block, jsonKey string) {
	cur := blockList(d[block])
	if len(cur) == 0 {
		return
	}
	priorItems := blockList(prior[block])
	priorList := jsonList(priorJSON[jsonKey])
	remoteList := jsonList(remote[jsonKey])
	if len(priorItems) != len(priorList) || len(cur) != len(remoteList) {
		return
	}

	index := map[string]int{}
	for i, p := range priorItems {
		index[key(i, p)] = i
	}
	out := make([]any, len(cur))
	for i, c := range cur {
		out[i] = c
		j, ok := index[key(i, c)]
		if !ok {
			continue
		}
		pj, okp := priorList[j].(map[string]any)
		rj, okr := remoteList[i].(map[string]any)
		if okp && okr {
			out[i] = reconcileElement(fields, priorItems[j], pj, c, rj)
		}
	}
	d[block] = out
}

func jsonList(v any) []any {
	if m, ok := v.(map[string]any); ok {
		v = m["list"]
	}
	items, _ := v.([]any)
	return items
}

func reconcilePanels(prior, d, remote map[string]any) {
	remotePanels, ok := decodePanels(remote["panels"])
	if !ok {
		return
	}
	priorPanels, err := layout(prior)
	if err != nil {
		return
	}
	byID := map[string]placed{}
	for _, p := range priorPanels {
		byID[panelID(p.model)] = p
		for _, c := range p.children {
			byID[panelID(c.model)] = c
		}
	}
	for i := range remotePanels {
		reconcilePlaced(&remotePanels[i], byID)
		for j := range remotePanels[i].children {
			reconcilePlaced(&remotePanels[i].children[j], byID)
		}
	}
	delete(d, "panel")
	delete(d, "row")
	setPanels(d, remotePanels)
}

func reconcilePlaced(p *placed, byID map[string]placed) {
	prior, ok := byID[panelID(p.model)]
	if !ok || (prior.model["type"] == "row") != (p.model["type"] == "row") {
		return
	}
	if p.model["type"] != "row" {
		p.tree = reconcileElement(panelFields, prior.tree, prior.model, p.tree, p.model)
		return
	}
	priorTree := maps.Clone(prior.tree)
	delete(priorTree, "panel")
	p.tree = reconcileElement(rowFields, priorTree, withoutPanels(prior.model), p.tree, withoutPanels(p.model))
}

func panelID(j map[string]any) string {
	return fmt.Sprint(normalize(j["id"]))
}

func withoutPanels(j map[string]any) map[string]any {
	out := maps.Clone(j)
	delete(out, "panels")
	return out
}
