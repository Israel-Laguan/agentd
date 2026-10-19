package httpx

import "net/http"

// DefaultMaxBody caps a JSON request body. Task, agent, project and
// preference payloads are a few KiB; 1 MiB leaves room for long descriptions.
const DefaultMaxBody int64 = 1 << 20

// LimitBody caps r.Body at max bytes. A decoder that reads past the cap gets
// an error, so the handler's existing "invalid JSON request body" branch
// rejects the request instead of buffering it.
func LimitBody(w http.ResponseWriter, r *http.Request, max int64) {
	if r.Body == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
}
