package jsonx

import (
	"encoding/json"
	"errors"
	"io"
)

// DecodeStrictLimit строго декодирует ровно один JSON-объект с запретом лишних полей.
func DecodeStrictLimit(r io.Reader, out any, maxBytes int64) error {
	if r == nil {
		return errors.New("nil reader")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	dec := json.NewDecoder(io.LimitReader(r, maxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("extra json values")
	}
	return nil
}
