package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxPlanBytes = 8 << 20

// Load reads exactly one bounded JSON artifact with exact field names, no
// duplicate/unknown keys, nulls or scalar coercion. Required fields must appear.
// Whitespace and JSON escape spelling are not significant; structure is.
func Load(r io.Reader) (Plan, error) {
	var p Plan
	b, err := io.ReadAll(io.LimitReader(r, MaxPlanBytes+1))
	if err != nil {
		return p, err
	}
	if len(b) > MaxPlanBytes {
		return p, fmt.Errorf("plan exceeds %d bytes", MaxPlanBytes)
	}
	if !utf8.Valid(b) {
		return p, fmt.Errorf("plan is not valid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = readValue(d, reflect.TypeOf(p), "plan", 0); err != nil {
		return p, err
	}
	if _, err = d.Token(); err != io.EOF {
		return p, fmt.Errorf("expected exactly one JSON plan")
	}
	strict := json.NewDecoder(bytes.NewReader(b))
	strict.DisallowUnknownFields()
	if err = strict.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("decode plan: %w", err)
	}
	if err = p.Validate(); err != nil {
		return Plan{}, err
	}
	return p, nil
}
func readValue(d *json.Decoder, t reflect.Type, path string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s: nesting too deep", path)
	}
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if token == nil {
		return fmt.Errorf("%s: null is not allowed", path)
	}
	switch t.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return fmt.Errorf("%s: expected object", path)
		}
		type field struct {
			typ      reflect.Type
			optional bool
		}
		fields := map[string]field{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")
			fields[tag[0]] = field{f.Type, len(tag) > 1 && tag[1] == "omitempty"}
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("%s: expected field name", path)
			}
			f, ok := fields[name]
			if !ok {
				return fmt.Errorf("%s: unknown field %q", path, name)
			}
			if seen[name] {
				return fmt.Errorf("%s: duplicate field %q", path, name)
			}
			seen[name] = true
			if err = readValue(d, f.typ, path+"."+name, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("%s: invalid object", path)
		}
		for name, f := range fields {
			if !f.optional && !seen[name] {
				return fmt.Errorf("%s: missing field %q", path, name)
			}
		}
	case reflect.Slice:
		if token != json.Delim('[') {
			return fmt.Errorf("%s: expected array", path)
		}
		for i := 0; d.More(); i++ {
			if err := readValue(d, t.Elem(), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("%s: invalid array", path)
		}
	case reflect.String:
		if _, ok := token.(string); !ok {
			return fmt.Errorf("%s: expected string", path)
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", path)
		}
	case reflect.Int:
		n, ok := token.(json.Number)
		if !ok {
			return fmt.Errorf("%s: expected integer", path)
		}
		if _, err := strconv.ParseInt(string(n), 10, 64); err != nil {
			return fmt.Errorf("%s: expected integer", path)
		}
	default:
		return fmt.Errorf("%s: unsupported field type", path)
	}
	return nil
}

// ID identifies the validated artifact independently of JSON whitespace.
func (p Plan) ID() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
