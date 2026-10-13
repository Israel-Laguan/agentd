package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/models"
)

func TestMaterializePlan_SendsStartEmptyWorkspace(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath string
		want       bool
	}{
		{name: "no source path starts empty so tasks are claimable", want: true},
		{name: "source path keeps the seeded flow", sourcePath: "/some/dir", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got models.DraftPlan
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Errorf("decode request: %v", err)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			defer srv.Close()
			plan := models.DraftPlan{ProjectName: "p", SourcePath: tt.sourcePath}
			if err := materializePlan(&cobra.Command{}, srv.Client(), srv.URL, config.Config{}, plan); err != nil {
				t.Fatalf("materializePlan() error = %v", err)
			}
			if got.StartEmptyWorkspace != tt.want {
				t.Fatalf("start_empty_workspace = %v, want %v", got.StartEmptyWorkspace, tt.want)
			}
		})
	}
}
