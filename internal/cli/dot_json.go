package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

// Validate object spelling and uniqueness before Go's case-insensitive struct
// decoder can merge fields. Walk arrays too, including message objects.
func dotCanonicalJSON(b []byte, t reflect.Type) error {
	d := json.NewDecoder(bytes.NewReader(b))
	if err := dotJSONValue(d, t); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func dotJSONValue(d *json.Decoder, t reflect.Type) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	if t.Kind() == reflect.Struct && t.PkgPath() != "time" {
		if tok != json.Delim('{') {
			return fmt.Errorf("expected object")
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f.Type
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("invalid key")
			}
			ft, ok := fields[name]
			if !ok || seen[name] {
				return fmt.Errorf("unknown or duplicate key")
			}
			seen[name] = true
			if err := dotJSONValue(d, ft); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if t.Kind() == reflect.Slice {
		if tok == nil {
			return nil
		}
		if tok != json.Delim('[') {
			return fmt.Errorf("expected array")
		}
		for d.More() {
			if err := dotJSONValue(d, t.Elem()); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	}
	if _, ok := tok.(json.Delim); ok {
		return fmt.Errorf("unexpected composite")
	}
	return nil
}
