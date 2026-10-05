package dashboard

import (
	"encoding/json"
	"fmt"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
)

const defaultSchemaVersion = 41

// Encode renders a dashboard tree (matching Body) as a Grafana dashboard model.
func Encode(d map[string]any) (map[string]any, error) {
	if schema.HasUnknown(d) {
		return nil, fmt.Errorf("dashboard contains unknown values")
	}
	out := encodeFields(dashboardFields, d)

	panels, err := layout(d)
	if err != nil {
		return nil, err
	}
	if len(panels) > 0 {
		out["panels"] = flatten(panels)
	}
	if err := encodeList(out, "templating", variableFields, d["variable"]); err != nil {
		return nil, err
	}
	if err := encodeList(out, "annotations", annotationFields, d["annotation"]); err != nil {
		return nil, err
	}
	links, err := encodeElements(linkFields, d["link"])
	if err != nil {
		return nil, err
	}
	if len(links) > 0 {
		out["links"] = links
	}
	if err := mergeExtra(out, d["extra"]); err != nil {
		return nil, err
	}
	if _, ok := out["schemaVersion"]; !ok {
		out["schemaVersion"] = json.Number(fmt.Sprint(defaultSchemaVersion))
	}
	return out, nil
}

func encodeFields(fields []field, m map[string]any) map[string]any {
	out := map[string]any{}
	for _, f := range fields {
		v := m[f.name]
		if v == nil {
			continue
		}
		if m, ok := v.(map[string]any); ok && f.kind == schema.Object {
			v = encodeObject(f.sub, m)
		}
		out[f.key] = v
	}
	return out
}

func encodeObject(attrs []schema.Attribute, m map[string]any) map[string]any {
	out := map[string]any{}
	for _, a := range attrs {
		if v := m[a.Name]; v != nil {
			out[a.Name] = v
		}
	}
	return out
}

func encodeElement(fields []field, m map[string]any) (map[string]any, error) {
	out := encodeFields(fields, m)
	return out, mergeExtra(out, m["extra"])
}

func encodeElements(fields []field, v any) ([]any, error) {
	var out []any
	for _, m := range blockList(v) {
		e, err := encodeElement(fields, m)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func encodeList(out map[string]any, key string, fields []field, v any) error {
	list, err := encodeElements(fields, v)
	if err != nil || len(list) == 0 {
		return err
	}
	out[key] = map[string]any{"list": list}
	return nil
}

func mergeExtra(out map[string]any, extra any) error {
	if extra == nil {
		return nil
	}
	m, ok := extra.(map[string]any)
	if !ok {
		return fmt.Errorf("extra must be an object")
	}
	for k, v := range m {
		if _, exists := out[k]; exists {
			return errExtraKey(k)
		}
		out[k] = v
	}
	return nil
}

func blockList(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func errExtraKey(key string) error {
	return fmt.Errorf("extra key %q is already set by another argument", key)
}
