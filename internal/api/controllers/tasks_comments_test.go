package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
)

func TestTaskHandler_ListComments(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	const body = "hello from test"
	if err := store.AddComment(context.Background(), models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorUser,
		Body:   body,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/comments", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		Data   []struct {
			TaskID  string `json:"TaskID"`
			Author  string `json:"Author"`
			Body    string `json:"Body"`
			Content string `json:"Content"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "success" {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(comments) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].TaskID != taskID {
		t.Fatalf("TaskID = %q, want %q", resp.Data[0].TaskID, taskID)
	}
	if resp.Data[0].Author != string(models.CommentAuthorUser) {
		t.Fatalf("Author = %q, want %q", resp.Data[0].Author, models.CommentAuthorUser)
	}
	if resp.Data[0].Body != body {
		t.Fatalf("Body = %q, want %q", resp.Data[0].Body, body)
	}
	if resp.Data[0].Content != body {
		t.Fatalf("Content = %q, want %q", resp.Data[0].Content, body)
	}
}

func TestTaskHandler_ListCommentsEmpty(t *testing.T) {
	h, store := taskTestHandler()
	_, taskID := seedProjectTask(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+taskID+"/comments", nil)
	req.SetPathValue("id", taskID)
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "[]" {
		t.Fatalf("data = %s, want []", resp.Data)
	}
}

func TestTaskHandler_ListCommentsNilStore(t *testing.T) {
	h := controllers.TaskHandler{Store: nil}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/t1/comments", nil)
	req.SetPathValue("id", "t1")
	rec := httptest.NewRecorder()
	h.ListComments(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListComments code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if string(resp.Data) != "[]" {
		t.Fatalf("data = %s, want []", resp.Data)
	}
}
