package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// DefaultMaxBody caps a JSON request body. Task, agent, project and
// preference payloads are a few KiB; 1 MiB leaves room for long descriptions.
const DefaultMaxBody int64 = 1 << 20

// LimitBody caps r.Body at max bytes. A reader that goes past the cap gets an
// error, so the request is rejected instead of buffered.
func LimitBody(w http.ResponseWriter, r *http.Request, max int64) {
	if r.Body == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
}

// DecodeJSON reads one JSON value from the capped body into v and then reads
// the rest of the body, so an excess that follows the value (even only
// whitespace) fails with the size error and trailing data is refused.
func DecodeJSON(w http.ResponseWriter, r *http.Request, max int64, v any) error {
	LimitBody(w, r, max)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON value")
	}
	return nil
}
