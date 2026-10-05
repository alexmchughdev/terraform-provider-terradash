package dashboard

import (
	"encoding/json"
	"math/big"
	"reflect"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
)

// Equal compares JSON values semantically. Nulls, empty strings, empty lists
// and empty objects inside objects are treated as absent.
func Equal(a, b any) bool {
	na, nb := normalize(a), normalize(b)
	return (isEmpty(na) && isEmpty(nb)) || reflect.DeepEqual(na, nb)
}

func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if n := normalize(e); !isEmpty(n) {
				out[k] = n
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case json.Number:
		if f, err := schema.ParseNumber(x); err == nil {
			return schema.NumberFromFloat(f)
		}
	case float64:
		return schema.NumberFromFloat(big.NewFloat(x))
	case int:
		return schema.NumberFromFloat(new(big.Float).SetInt64(int64(x)))
	}
	return v
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case map[string]any:
		return len(x) == 0
	case []any:
		return len(x) == 0
	}
	return false
}
