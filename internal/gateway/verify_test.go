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
	check, skipped := FirstWorking(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", CodexWire: "responses", Timeout: 5 * time.Second}, models, "gpt", "responses")
	if !check.OK || check.Rejected() || check.Detail != "gpt-5.4" {
		t.Fatalf("check %+v skipped %+v", check, skipped)
	}
	if len(skipped) != 1 || skipped[0].ID != "gpt-5.5" || !strings.Contains(skipped[0].Check.Hint, "503") {
		t.Fatalf("skipped %+v", skipped)
	}
	if skipped[0].Check.Message != "no available channel" {
		t.Fatalf("gateway message %q", skipped[0].Check.Message)
	}
}

func TestFirstWorkingStopsOnQuotaRefusal(t *testing.T) {
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		http.Error(w, `{"error":{"message":"用户额度不足"}}`, http.StatusForbidden)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-4o"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check, skipped := FirstWorking(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 5 * time.Second}, models, "gpt", "chat")
	if check.OK || !check.Rejected() || check.Status != http.StatusForbidden || len(skipped) != 0 || posts != 1 {
		t.Fatalf("check %+v skipped %+v posts %d", check, skipped, posts)
	}
	if check.Message != "用户额度不足" {
		t.Fatalf("gateway message %q", check.Message)
	}
	one := ProbeID(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 5 * time.Second}, "gpt", "chat", "gpt-4o")
	if one.OK || one.Status != http.StatusForbidden || one.Detail != "gpt-4o" {
		t.Fatalf("probe %+v", one)
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
