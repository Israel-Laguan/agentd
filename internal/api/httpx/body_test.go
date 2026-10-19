package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, body string, max int64) (map[string]int, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var out map[string]int
	err := DecodeJSON(httptest.NewRecorder(), req, max, &out)
	return out, err
}

func TestDecodeJSONAcceptsBodyWithinLimit(t *testing.T) {
	out, err := decodeBody(t, `{"a":1}`+"\n", 64)
	if err != nil || out["a"] != 1 {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

// A small valid value followed by an oversized suffix must be rejected: a
// bare Decode would stop after the value and never read the excess.
func TestDecodeJSONRejectsOversizedWhitespaceSuffix(t *testing.T) {
	if _, err := decodeBody(t, `{"a":1}`+strings.Repeat(" ", 200), 64); err == nil {
		t.Fatal("want error for a body that exceeds the limit through trailing whitespace")
	}
}

func TestDecodeJSONRejectsTrailingData(t *testing.T) {
	if _, err := decodeBody(t, `{"a":1} {"b":2}`, 64); err == nil {
		t.Fatal("want error for data after the JSON value")
	}
}
