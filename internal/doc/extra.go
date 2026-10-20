package doc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// Extra holds the JSON fields of a model object that the Go port doesn't model yet, verbatim
// (compacted), so a document read from the Rust app's JSON is written back unchanged. As types
// grow, fields move from Extra to typed struct fields.
type Extra map[string]json.RawMessage

// Equal reports whether two Extras hold the same fields with the same (compact) JSON.
func (e Extra) Equal(o Extra) bool {
	return maps.EqualFunc(e, o, func(a, b json.RawMessage) bool { return bytes.Equal(a, b) })
}

// Clone copies the map (the raw values are never modified in place, so they're shared).
func (e Extra) Clone() Extra {
	if len(e) == 0 {
		return nil
	}
	return maps.Clone(e)
}

// Get decodes the field name into v; false when it is absent or doesn't decode.
func (e Extra) Get(name string, v any) bool {
	raw, ok := e[name]
	return ok && json.Unmarshal(raw, v) == nil
}

// set stores v under name, or removes name when v is nil.
func (e *Extra) set(name string, raw json.RawMessage) {
	if raw == nil {
		delete(*e, name)
		if len(*e) == 0 {
			*e = nil
		}
		return
	}
	if *e == nil {
		*e = Extra{}
	}
	(*e)[name] = raw
}

// knownKeys lists the JSON names of a struct type's fields (its `json` tags).
var knownKeys sync.Map // reflect.Type → []string

func keysOf(t reflect.Type) []string {
	if k, ok := knownKeys.Load(t); ok {
		if keys, ok := k.([]string); ok {
			return keys
		}
	}
	var keys []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	knownKeys.Store(t, keys)
	return keys
}

// unmarshalWithExtra decodes data into the typed fields of *v (a struct without its own
// UnmarshalJSON) and returns every other field in an Extra.
func unmarshalWithExtra[T any](data []byte, v *T) (Extra, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	if all == nil {
		return nil, fmt.Errorf("doc: expected a JSON object, got %.20s", data)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	for _, k := range keysOf(reflect.TypeFor[T]()) {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	for k, raw := range all {
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return nil, err
		}
		all[k] = b.Bytes()
	}
	return all, nil
}

// marshalWithExtra encodes the typed fields of v and adds the Extra fields (typed fields win).
// Keys are written sorted, so output is deterministic.
func marshalWithExtra[T any](v *T, extra Extra) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil || len(extra) == 0 {
		return b, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for _, k := range slices.Sorted(maps.Keys(extra)) {
		if _, typed := m[k]; !typed {
			m[k] = extra[k]
		}
	}
	return json.Marshal(m)
}
