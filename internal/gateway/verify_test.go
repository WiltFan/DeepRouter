package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseModelsRecommendedField(t *testing.T) {
	models, err := ParseModels([]byte(`{
		"recommended_model": "gpt-5.4",
		"data": [
			{"id": "gpt-5.5", "recommended": true},
			{"id": "gpt-5.4"},
			{"id": "claude-sonnet-4-6", "is_recommended": "yes"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	order := CandidateOrder(models, "gpt")
	if len(order) < 2 || order[0] != "gpt-5.5" {
		t.Fatalf("order %v", order)
	}
	if id := PickDefaultModels(models, "claude"); id != "claude-sonnet-4-6" {
		t.Fatalf("claude %s", id)
	}
}

func TestFirstWorkingSkipsUnavailableChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = readJSON(r, &body)
		if body.Model == "gpt-5.5" {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok"}`))
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"recommended_model":"gpt-5.5","data":[{"id":"gpt-5.5","recommended":true},{"id":"gpt-5.4"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, auth, hint, skipped := FirstWorking(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", CodexWire: "responses", Timeout: 5 * time.Second}, models, "gpt", "responses")
	if auth || id != "gpt-5.4" {
		t.Fatalf("id %q auth %v hint %q skipped %+v", id, auth, hint, skipped)
	}
	if len(skipped) != 1 || skipped[0].ID != "gpt-5.5" || !strings.Contains(skipped[0].Hint, "503") {
		t.Fatalf("skipped %+v", skipped)
	}
}

func readJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}
