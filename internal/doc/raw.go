package doc

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/computernut43/craft-libs/serdejson"
)

// Raw is the JSON of a part of the model the Go port doesn't type yet (inline objects, a
// paragraph's section), kept as read (compacted) and written back as it is.
// Where Rust would refuse or normalise such a part (a wrong type, a number written 1 for 1.0, a
// missing field it fills with its default, an unknown field it drops), Raw keeps what the file
// says: it is checked only as JSON. MIGRATION.md lists these parts; they shrink as types land.
type Raw []byte

// DecodeSerde keeps the value's JSON.
func (r *Raw) DecodeSerde(d serdejson.Decoder) error {
	b, err := d.Raw()
	if err != nil {
		return err
	}
	var c bytes.Buffer
	if err := json.Compact(&c, b); err != nil {
		return d.Fail("invalid JSON: " + err.Error())
	}
	*r = c.Bytes()
	return nil
}

// MarshalJSON writes the JSON as read.
func (r Raw) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return r, nil
}

// Get decodes the JSON leniently (encoding/json) into v, for reading a detail of an untyped
// part; false when it is absent or doesn't decode.
func (r Raw) Get(v any) bool { return r != nil && json.Unmarshal(r, v) == nil }

// WithoutKey is the object r without its key k, the other keys in their order.
func (r Raw) WithoutKey(k string) (Raw, error) {
	dec := json.NewDecoder(bytes.NewReader(r))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("doc: not a JSON object")
	}
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := t.(string)
		if !ok {
			return nil, errors.New("doc: object key is not a string")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if key == k {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		kb, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(kb)
		out.WriteByte(':')
		out.Write(v)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
