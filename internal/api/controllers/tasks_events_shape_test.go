package controllers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"agentd/internal/models"
)

// The J06 UI tests render a static capture of GET /api/v1/tasks/{id}/events.
// This test builds the same event types through the real handler and fails when
// the response no longer has the fixture's shape (keys and value types, in the
// envelope and in each event), so an endpoint change cannot leave the UI tests
// passing on stale data. Values (ids, timestamps) are not compared.
const taskEventsFixturePath = "../../../web/app/components/task/__fixtures__/task-events.json"

func TestTaskEventsResponseMatchesUIFixture(t *testing.T) {
	raw, err := os.ReadFile(taskEventsFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	h, store := taskTestHandler()
	projectID, taskID := seedProjectTask(t, store)
	for i, ev := range fixtureEvents(t, fixture) {
		err := store.AppendEvent(context.Background(), models.Event{
			BaseEntity: models.BaseEntity{ID: "shape-" + string(rune('a'+i))},
			ProjectID:  projectID,
			TaskID:     sql.NullString{String: taskID, Valid: true},
			Type:       models.EventType(ev["type"].(string)),
			Payload:    ev["payload"].(string),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/events", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("events code = %d body = %s", rec.Code, rec.Body.String())
	}
	var live map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"status", "meta"} {
		assertSameShape(t, key, shapeOf(fixture[key]), shapeOf(live[key]))
	}
	liveByType := eventsByType(t, live)
	for typ, want := range eventsByType(t, fixture) {
		got, ok := liveByType[typ]
		if !ok {
			t.Fatalf("live response has no %s event", typ)
		}
		assertSameShape(t, "data["+typ+"]", shapeOf(want), shapeOf(got))
	}
}

func fixtureEvents(t *testing.T, env map[string]any) []map[string]any {
	t.Helper()
	data, ok := env["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("envelope has no data array: %v", env["data"])
	}
	out := make([]map[string]any, 0, len(data))
	for _, item := range data {
		out = append(out, item.(map[string]any))
	}
	return out
}

func eventsByType(t *testing.T, env map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, ev := range fixtureEvents(t, env) {
		out[ev["type"].(string)] = ev
	}
	return out
}

// shapeOf reduces a decoded JSON value to its keys and value types.
func shapeOf(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = shapeOf(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, val := range x {
			out = append(out, shapeOf(val))
		}
		return out
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	}
	return "unknown"
}

func assertSameShape(t *testing.T, name string, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("%s drifted from the UI fixture\nfixture: %v\nhandler: %v", name, want, got)
	}
}
