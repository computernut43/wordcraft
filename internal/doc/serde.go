package doc

import (
	"fmt"
	"math"
	"reflect"

	"github.com/computernut43/craft-libs/serdejson"
)

// F32 is a Rust f32 field: written as serde_json writes one (12.0, not 12).
type F32 = serdejson.F32

// Vec is a Rust Vec field: written as [] when empty.
type Vec[T any] = serdejson.Vec[T]

// readEnum reads a fieldless Rust enum (serde's derived Deserialize): "name", or {"name": null}.
func readEnum[T ~uint8](d serdejson.Decoder, names []string, v *T) error {
	i, err := d.UnitVariant(names)
	if err != nil {
		return err
	}
	*v = T(i)
	return nil
}

// writeEnum writes a fieldless Rust enum as its variant's name (names are plain identifiers).
func writeEnum[T ~uint8](names []string, v T) ([]byte, error) {
	if int(v) >= len(names) {
		return nil, fmt.Errorf("doc: no variant %d of %T", v, v)
	}
	return []byte(`"` + names[v] + `"`), nil
}

// enumName is a variant's name, or "" for a value out of range.
func enumName[T ~uint8](names []string, v T) string {
	if int(v) >= len(names) {
		return ""
	}
	return names[v]
}

// f32 rounds a float64 to the nearest f32, as Rust's `as f32` does.
func f32(v float64) F32 { return F32(float32(v)) }

// finite reports whether v is neither infinite nor NaN.
func finite(v F32) bool { return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v)) }

// equal compares two model values as Rust's derived PartialEq does: field by field, through
// pointers (Option, Box), with floats compared by value (so NaN is unequal to itself), and an
// empty Vec equal to a nil one.
func equal(a, b any) bool { return equalValue(reflect.ValueOf(a), reflect.ValueOf(b)) }

func equalValue(a, b reflect.Value) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return equalValue(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			if !equalValue(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !equalValue(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		it := a.MapRange()
		for it.Next() {
			w := b.MapIndex(it.Key())
			if !w.IsValid() || !equalValue(it.Value(), w) {
				return false
			}
		}
		return true
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.String:
		return a.String() == b.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return a.Uint() == b.Uint()
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return equalValue(a.Elem(), b.Elem())
	}
	return false
}

// set copies an Option field of a patch onto dst when the patch sets it (Rust's
// `if src.is_some() { dst = src.clone() }`). Model values are never changed through a pointer,
// only replaced, so sharing the patch's value is safe.
func set[T any](dst **T, src *T) {
	if src != nil {
		*dst = src
	}
}

// dropSame unsets an Option field equal to the base's (Rust's
// `if dst == base { dst = None }`).
func dropSame[T any](dst **T, base *T) {
	if equal(*dst, base) {
		*dst = nil
	}
}

func ptr[T any](v T) *T { return &v }

// readTuple reads a Rust tuple (or tuple variant) into elems: a sequence of exactly len(elems)
// values.
func readTuple(d serdejson.Decoder, elems ...any) error {
	if d.Peek() != serdejson.KindArray {
		return d.Fail(fmt.Sprintf("invalid type, expected a tuple of size %d", len(elems)))
	}
	n := 0
	err := d.Array(func(i int) error {
		if i >= len(elems) {
			return d.Fail(fmt.Sprintf("trailing characters (expected a tuple of size %d)", len(elems)))
		}
		n = i + 1
		return d.Decode(elems[i])
	})
	if err != nil {
		return err
	}
	if n != len(elems) {
		return d.Fail(fmt.Sprintf("invalid length %d, expected a tuple of size %d", n, len(elems)))
	}
	return nil
}

// tagged writes an internally tagged variant: {"tag": "name", fields…}, fields being a struct.
func tagged(tag, name string, fields any) ([]byte, error) {
	out := []byte(`{"` + tag + `":"` + name + `"`)
	if fields == nil {
		return append(out, '}'), nil
	}
	b, err := serdejson.MarshalCompact(fields)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != '{' {
		return nil, fmt.Errorf("doc: variant %s is not a struct", name)
	}
	if len(b) > 2 {
		out = append(out, ',')
	}
	return append(out, b[1:]...), nil
}
