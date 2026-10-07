package hclgen

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
)

// A tree value of type hclwrite.Tokens is written verbatim as an expression.

// Options controls how values are written.
type Options struct {
	// Variables maps Grafana "${NAME}" placeholders to Terraform variable names.
	Variables map[string]string
}

func Resource(body *hclwrite.Body, resourceType, name string, b schema.Block, m map[string]any, opts Options) {
	block := body.AppendNewBlock("resource", []string{resourceType, name})
	writeBlock(block.Body(), b, m, opts)
}

func Import(body *hclwrite.Body, resourceType, name, id string) {
	block := body.AppendNewBlock("import", nil)
	block.Body().SetAttributeTraversal("to", hcl.Traversal{
		hcl.TraverseRoot{Name: resourceType},
		hcl.TraverseAttr{Name: name},
	})
	block.Body().SetAttributeValue("id", cty.StringVal(id))
}

func Variable(body *hclwrite.Body, name, description string, def *string) {
	block := body.AppendNewBlock("variable", []string{name})
	block.Body().SetAttributeRaw("type", hclwrite.Tokens{ident("string")})
	if description != "" {
		block.Body().SetAttributeValue("description", cty.StringVal(description))
	}
	if def != nil {
		block.Body().SetAttributeValue("default", cty.StringVal(*def))
	}
}

func writeBlock(body *hclwrite.Body, b schema.Block, m map[string]any, opts Options) {
	for _, a := range b.Attributes {
		v := m[a.Name]
		if v == nil || (a.Computed && !a.Optional) {
			continue
		}
		if m, ok := v.(map[string]any); ok && a.Kind == schema.Object {
			body.SetAttributeRaw(a.Name, objectTokens(m, attributeNames(a.Attributes), true, opts))
			continue
		}
		body.SetAttributeRaw(a.Name, valueTokens(v, opts))
	}
	for _, nb := range b.Blocks {
		items, _ := m[nb.Name].([]any)
		for _, item := range items {
			child, ok := item.(map[string]any)
			if !ok {
				continue
			}
			body.AppendNewline()
			writeBlock(body.AppendNewBlock(nb.Name, nil).Body(), nb, child, opts)
		}
	}
}

func attributeNames(attrs []schema.Attribute) []string {
	names := make([]string, len(attrs))
	for i, a := range attrs {
		names[i] = a.Name
	}
	return names
}

func valueTokens(v any, opts Options) hclwrite.Tokens {
	switch x := v.(type) {
	case hclwrite.Tokens:
		return x
	case string:
		return stringTokens(x, opts)
	case json.Number:
		return hclwrite.Tokens{{Type: hclsyntax.TokenNumberLit, Bytes: []byte(x)}}
	case bool:
		return hclwrite.TokensForValue(cty.BoolVal(x))
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return objectTokens(x, keys, false, opts)
	case []any:
		return listTokens(x, opts)
	}
	return hclwrite.Tokens{ident("null")}
}

func objectTokens(m map[string]any, keys []string, skipNull bool, opts Options) hclwrite.Tokens {
	toks := hclwrite.Tokens{{Type: hclsyntax.TokenOBrace, Bytes: []byte("{")}}
	written := 0
	for _, k := range keys {
		v, ok := m[k]
		if !ok || (v == nil && skipNull) {
			continue
		}
		toks = append(toks, newline())
		toks = append(toks, keyTokens(k)...)
		toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenEqual, Bytes: []byte("=")})
		toks = append(toks, valueTokens(v, opts)...)
		written++
	}
	if written > 0 {
		toks = append(toks, newline())
	}
	return append(toks, &hclwrite.Token{Type: hclsyntax.TokenCBrace, Bytes: []byte("}")})
}

var reservedKeys = []string{"null", "true", "false", "for", "if", "in"}

func keyTokens(k string) hclwrite.Tokens {
	if hclsyntax.ValidIdentifier(k) && !slices.Contains(reservedKeys, k) {
		return hclwrite.Tokens{ident(k)}
	}
	return quoted(k, nil)
}

func listTokens(items []any, opts Options) hclwrite.Tokens {
	toks := hclwrite.Tokens{{Type: hclsyntax.TokenOBrack, Bytes: []byte("[")}}
	multiline := !allScalars(items)
	for i, item := range items {
		if multiline {
			toks = append(toks, newline())
		} else if i > 0 {
			toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenComma, Bytes: []byte(",")})
		}
		toks = append(toks, valueTokens(item, opts)...)
		if multiline {
			toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenComma, Bytes: []byte(",")})
		}
	}
	if multiline && len(items) > 0 {
		toks = append(toks, newline())
	}
	return append(toks, &hclwrite.Token{Type: hclsyntax.TokenCBrack, Bytes: []byte("]")})
}

func allScalars(items []any) bool {
	for _, item := range items {
		switch x := item.(type) {
		case map[string]any, []any:
			return false
		case string:
			if strings.Contains(x, "\n") {
				return false
			}
		}
	}
	return true
}

func stringTokens(s string, opts Options) hclwrite.Tokens {
	if name, ok := opts.Variables[placeholder(s)]; ok {
		return variableRef(name)
	}
	if useHeredoc(s) {
		return heredoc(s, opts.Variables)
	}
	return quoted(s, opts.Variables)
}

func placeholder(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return s[2 : len(s)-1]
	}
	return ""
}

func useHeredoc(s string) bool {
	if !strings.Contains(strings.TrimRight(s, "\n"), "\n") {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\n' && r != '\t' })
}

func quoted(s string, vars map[string]string) hclwrite.Tokens {
	toks := hclwrite.Tokens{{Type: hclsyntax.TokenOQuote, Bytes: []byte(`"`)}}
	toks = append(toks, templateTokens(s, vars, escapeQuoted)...)
	return append(toks, &hclwrite.Token{Type: hclsyntax.TokenCQuote, Bytes: []byte(`"`)})
}

// heredoc writes multi-line strings readably. A string without a trailing
// newline is wrapped in chomp() to keep it exact.
func heredoc(s string, vars map[string]string) hclwrite.Tokens {
	body := s
	if !strings.HasSuffix(s, "\n") {
		body += "\n"
	}
	delim := heredocDelimiter(body)
	toks := hclwrite.Tokens{{Type: hclsyntax.TokenOHeredoc, Bytes: []byte("<<" + delim + "\n")}}
	toks = append(toks, templateTokens(body, vars, escapeTemplate)...)
	toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenCHeredoc, Bytes: []byte(delim)})
	if strings.HasSuffix(s, "\n") {
		return toks
	}
	call := hclwrite.Tokens{ident("chomp"), {Type: hclsyntax.TokenOParen, Bytes: []byte("(")}}
	call = append(call, toks...)
	call = append(call, newline())
	return append(call, &hclwrite.Token{Type: hclsyntax.TokenCParen, Bytes: []byte(")")})
}

func heredocDelimiter(s string) string {
	lines := strings.Split(s, "\n")
	for i := 0; ; i++ {
		delim := "EOT"
		if i > 0 {
			delim = fmt.Sprintf("EOT%d", i)
		}
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.TrimSpace(l) == delim }) {
			return delim
		}
	}
}

func templateTokens(s string, vars map[string]string, escape func(string) string) hclwrite.Tokens {
	var toks hclwrite.Tokens
	for s != "" {
		start, end, name := nextPlaceholder(s, vars)
		if start < 0 {
			break
		}
		toks = append(toks, literalBefore(s[:start], escape)...)
		toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")})
		toks = append(toks, variableRef(name)...)
		toks = append(toks, &hclwrite.Token{Type: hclsyntax.TokenTemplateSeqEnd, Bytes: []byte("}")})
		s = s[end:]
	}
	if s != "" {
		toks = append(toks, literal(escape(s)))
	}
	return toks
}

// literalBefore writes text preceding an interpolation. A trailing "$" would
// form the "$${" escape, so it is emitted as ${"$"} instead.
func literalBefore(s string, escape func(string) string) hclwrite.Tokens {
	if !strings.HasSuffix(s, "$") {
		return hclwrite.Tokens{literal(escape(s))}
	}
	return hclwrite.Tokens{
		literal(escape(s[:len(s)-1])),
		{Type: hclsyntax.TokenTemplateInterp, Bytes: []byte("${")},
		{Type: hclsyntax.TokenOQuote, Bytes: []byte(`"`)},
		literal("$"),
		{Type: hclsyntax.TokenCQuote, Bytes: []byte(`"`)},
		{Type: hclsyntax.TokenTemplateSeqEnd, Bytes: []byte("}")},
	}
}

func nextPlaceholder(s string, vars map[string]string) (start, end int, name string) {
	offset := 0
	for {
		i := strings.Index(s[offset:], "${")
		if i < 0 {
			return -1, -1, ""
		}
		i += offset
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return -1, -1, ""
		}
		if v, ok := vars[s[i+2:i+j]]; ok {
			return i, i + j + 1, v
		}
		offset = i + 2
	}
}

func escapeTemplate(s string) string {
	s = strings.ReplaceAll(s, "${", "$${")
	return strings.ReplaceAll(s, "%{", "%%{")
}

func escapeQuoted(s string) string {
	var b strings.Builder
	for _, r := range escapeTemplate(s) {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func variableRef(name string) hclwrite.Tokens {
	return hclwrite.TokensForTraversal(hcl.Traversal{hcl.TraverseRoot{Name: "var"}, hcl.TraverseAttr{Name: name}})
}

func literal(s string) *hclwrite.Token {
	return &hclwrite.Token{Type: hclsyntax.TokenQuotedLit, Bytes: []byte(s)}
}

func ident(s string) *hclwrite.Token {
	return &hclwrite.Token{Type: hclsyntax.TokenIdent, Bytes: []byte(s)}
}

func newline() *hclwrite.Token {
	return &hclwrite.Token{Type: hclsyntax.TokenNewline, Bytes: []byte("\n")}
}
