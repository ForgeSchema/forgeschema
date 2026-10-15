// Package strictyaml implements the restricted YAML subset shared by schema and rename files.
package strictyaml

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"reflect"
	"strings"
)

const MaxBytes = 1 << 20

func Decode(r io.Reader, out any) error {
	t := reflect.TypeOf(out)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() || t.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("decoder requires a nonnil struct pointer")
	}
	b, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return err
	}
	if len(b) > MaxBytes {
		return fmt.Errorf("document exceeds %d bytes", MaxBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var n yaml.Node
	if err = dec.Decode(&n); err != nil {
		return fmt.Errorf("read document: %w", err)
	}
	if len(n.Content) != 1 {
		return fmt.Errorf("document must be a mapping")
	}
	if err = checkNode(n.Content[0], t.Elem(), "document", 0); err != nil {
		return err
	}
	var extra yaml.Node
	if err = dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one YAML document")
	}
	strict := yaml.NewDecoder(bytes.NewReader(b))
	strict.KnownFields(true)
	if err = strict.Decode(out); err != nil {
		return fmt.Errorf("decode document: %w", err)
	}
	return nil
}
func checkNode(n *yaml.Node, t reflect.Type, path string, depth int) error {
	if depth > 32 {
		return fmt.Errorf("%s: nesting too deep", path)
	}
	if n.Anchor != "" || n.Kind == yaml.AliasNode {
		return fmt.Errorf("%s: YAML anchors and aliases are unsupported", path)
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode || n.Tag != "!!map" {
			return fmt.Errorf("%s: expected mapping", path)
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields[strings.Split(f.Tag.Get("yaml"), ",")[0]] = f.Type
		}
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Anchor != "" || k.Kind == yaml.AliasNode {
				return fmt.Errorf("%s: YAML anchors and aliases are unsupported", path)
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return fmt.Errorf("%s: expected string field name", path)
			}
			ft, ok := fields[k.Value]
			if !ok {
				return fmt.Errorf("%s: unknown field %q", path, k.Value)
			}
			if seen[k.Value] {
				return fmt.Errorf("%s: duplicate field %q", path, k.Value)
			}
			seen[k.Value] = true
			if err := checkNode(v, ft, path+"."+k.Value, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode || n.Tag != "!!seq" {
			return fmt.Errorf("%s: expected sequence", path)
		}
		for i, v := range n.Content {
			if err := checkNode(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	default:
		tag := "!!str"
		if t.Kind() == reflect.Int {
			tag = "!!int"
		}
		if t.Kind() == reflect.Bool {
			tag = "!!bool"
		}
		if n.Kind != yaml.ScalarNode || n.Tag != tag {
			return fmt.Errorf("%s: expected %s scalar", path, strings.TrimPrefix(tag, "!!"))
		}
	}
	return nil
}
