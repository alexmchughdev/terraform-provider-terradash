package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"go.yaml.in/yaml/v3"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/hclgen"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/tfschema"
)

type Source struct {
	Model     map[string]any
	FolderUID string
}

func (s Source) Title() string {
	title, _ := s.Model["title"].(string)
	return title
}

type Options struct {
	Import    bool
	FolderUID string
}

type Variable struct {
	Description string
	Default     *string
}

type Output struct {
	Name      string
	HCL       []byte
	Variables map[string]Variable
	Warnings  []string
}

// Load parses a dashboard model, a GET /api/dashboards response or a
// dashboard.grafana.app resource, as JSON or YAML.
func Load(data []byte) (Source, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return Source{}, err
	}
	if model, ok := doc["dashboard"].(map[string]any); ok {
		meta, _ := doc["meta"].(map[string]any)
		folder, _ := meta["folderUid"].(string)
		return Check(Source{Model: model, FolderUID: folder})
	}
	if doc["kind"] == "Dashboard" {
		return fromResource(doc)
	}
	return Check(Source{Model: doc})
}

func parseDocument(data []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("empty input")
	}
	if !bytes.HasPrefix(trimmed, []byte("{")) {
		converted, err := yamlToJSON(data)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON or YAML: %w", err)
		}
		trimmed = converted
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return doc, nil
}

// yamlToJSON uses YAML 1.2 rules, so keys like y and no stay strings.
func yamlToJSON(data []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func fromResource(doc map[string]any) (Source, error) {
	if v, _ := doc["apiVersion"].(string); strings.Contains(v, "/v2") {
		return Source{}, fmt.Errorf("dashboard schema %s is not supported; export the dashboard as classic JSON", v)
	}
	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		return Source{}, errors.New("dashboard resource has no spec")
	}
	model := maps.Clone(spec)
	meta, _ := doc["metadata"].(map[string]any)
	if name, ok := meta["name"].(string); ok && model["uid"] == nil {
		model["uid"] = name
	}
	annotations, _ := meta["annotations"].(map[string]any)
	folder, _ := annotations["grafana.app/folder"].(string)
	return Check(Source{Model: model, FolderUID: folder})
}

func Check(src Source) (Source, error) {
	if _, ok := src.Model["elements"]; ok {
		return Source{}, errors.New("v2 dashboards are not supported; export the dashboard as classic JSON")
	}
	if _, ok := src.Model["title"].(string); !ok {
		return Source{}, errors.New("not a Grafana dashboard: missing title")
	}
	return src, nil
}

// Tree decodes a source into a resource tree. Placeholders map Grafana
// __inputs names to the Terraform variables declared in vars.
func Tree(src Source, opts Options) (t map[string]any, placeholders map[string]string, vars map[string]Variable) {
	model := maps.Clone(src.Model)
	placeholders, vars = inputVariables(model)
	t = dashboard.Decode(model)
	t["folder_uid"] = nonEmpty(src.FolderUID)
	if opts.FolderUID != "" {
		t["folder_uid"] = opts.FolderUID
	}
	return t, placeholders, vars
}

func Convert(src Source, name string, opts Options) Output {
	t, placeholders, vars := Tree(src, opts)
	out := Output{Name: name, Variables: vars}

	f := hclwrite.NewEmptyFile()
	uid, _ := t["uid"].(string)
	switch {
	case opts.Import && uid != "":
		hclgen.Import(f.Body(), tfschema.ResourceType, name, uid)
		f.Body().AppendNewline()
	case opts.Import:
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s: no uid, skipping import block", name))
	}
	hclgen.Resource(f.Body(), tfschema.ResourceType, name, tfschema.Resource, t, hclgen.Options{Variables: placeholders})
	out.HCL = hclwrite.Format(f.Bytes())
	return out
}

func nonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// inputVariables turns export-for-sharing __inputs into Terraform variables.
func inputVariables(model map[string]any) (map[string]string, map[string]Variable) {
	inputs, _ := model["__inputs"].([]any)
	placeholders := map[string]string{}
	vars := map[string]Variable{}
	for _, in := range inputs {
		m, _ := in.(map[string]any)
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		varName := Identifier(name)
		placeholders[name] = varName
		v := Variable{Description: describeInput(m)}
		if value, ok := m["value"].(string); ok && m["type"] == "constant" {
			v.Default = &value
		}
		vars[varName] = v
	}
	if len(inputs) > 0 {
		delete(model, "__inputs")
		delete(model, "__requires")
		delete(model, "__elements")
	}
	return placeholders, vars
}

func describeInput(m map[string]any) string {
	label, _ := m["label"].(string)
	desc, _ := m["description"].(string)
	plugin, _ := m["pluginName"].(string)
	parts := slices.DeleteFunc([]string{label, desc, plugin}, func(s string) bool { return s == "" })
	return strings.Join(slices.Compact(parts), " - ")
}

func VariablesHCL(vars map[string]Variable) []byte {
	f := hclwrite.NewEmptyFile()
	for i, name := range slices.Sorted(maps.Keys(vars)) {
		if i > 0 {
			f.Body().AppendNewline()
		}
		hclgen.Variable(f.Body(), name, vars[name].Description, vars[name].Default)
	}
	return hclwrite.Format(f.Bytes())
}

// Identifier converts text into a valid, lower-case Terraform identifier.
func Identifier(s string) string {
	var b strings.Builder
	underscore := false
	for _, r := range strings.ToLower(s) {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		switch {
		case alnum:
			b.WriteRune(r)
		case !underscore:
			b.WriteByte('_')
		}
		underscore = !alnum
	}
	id := strings.Trim(b.String(), "_")
	if id == "" || (id[0] >= '0' && id[0] <= '9') {
		id = "d_" + id
	}
	if len(id) > 64 {
		id = id[:64]
	}
	return strings.TrimRight(id, "_")
}

// Namer hands out unique resource names. "variables" is reserved for the
// generated variables.tf.
type Namer struct {
	used map[string]bool
}

func (n *Namer) Name(title string) string {
	if n.used == nil {
		n.used = map[string]bool{"variables": true}
	}
	base := Identifier(title)
	name := base
	for i := 2; n.used[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	n.used[name] = true
	return name
}
