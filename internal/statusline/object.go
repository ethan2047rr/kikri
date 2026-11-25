package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// object is a JSON object that keeps its keys in order and its values as
// they were, so editing one key of settings.json leaves the rest alone.
type object []member

type member struct {
	Key   string
	Value json.RawMessage
}

func (o object) get(k string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.Key == k {
			return m.Value, true
		}
	}
	return nil, false
}

func (o *object) set(k string, v json.RawMessage) {
	for i := range *o {
		if (*o)[i].Key == k {
			(*o)[i].Value = v
			return
		}
	}
	*o = append(*o, member{k, v})
}

func (o *object) del(k string) {
	out := (*o)[:0]
	for _, m := range *o {
		if m.Key != k {
			out = append(out, m)
		}
	}
	*o = out
}

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.Key)
		b.Write(k)
		b.WriteByte(':')
		b.Write(m.Value)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func parseObject(data []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("not a JSON object")
	}
	var o object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, member{k, v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

// readObject reads settings.json; a missing or empty file is an empty object.
func readObject(path string) (object, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(b)) == 0) {
		return object{}, nil
	}
	if err != nil {
		return nil, err
	}
	o, err := parseObject(b)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v); Kikri leaves it alone", path, err)
	}
	return o, nil
}

// writeObject writes settings.json with two-space indentation, through a
// symlink if it is one, keeping the file's permissions.
func writeObject(path string, o object) error {
	compact, _ := o.MarshalJSON()
	var b bytes.Buffer
	if err := json.Indent(&b, compact, "", "  "); err != nil {
		return err
	}
	b.WriteByte('\n')
	target := path
	if t, err := filepath.EvalSymlinks(path); err == nil {
		target = t
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(target); err == nil {
		mode = fi.Mode().Perm()
	}
	return writeAtomic(target, b.Bytes(), mode)
}
