package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseModelsRecommendedField(t *testing.T) {
	models, err := ParseModels([]byte(`{
		"recommended_model": "gpt-5.4",
		"data": [
			{"id": "gpt-5.5"},
			{"id": "gpt-5.4", "recommended": true},
			{"id": "claude-sonnet-4-6", "is_recommended": "yes"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	order := CandidateOrder(models, "gpt")
	if len(order) < 2 || order[0] != "gpt-5.5" || order[1] != "gpt-5.4" {
		t.Fatalf("newest should stay ahead of the recommended older id: %v", order)
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
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check, skipped := FirstWorking(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 5 * time.Second}, models, "gpt", "chat")
	if check.OK || !check.Rejected() || check.Status != http.StatusForbidden || len(skipped) != 0 || posts != 2 {
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

func TestVerifiedModelsKeepsOnlySuccesses(t *testing.T) {
	var mu sync.Mutex
	got := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = readJSON(r, &body)
		mu.Lock()
		got[body.Model]++
		mu.Unlock()
		switch body.Model {
		case "gpt-6.1-terra":
			http.Error(w, `{"error":{"message":"forbidden"}}`, http.StatusForbidden)
		case "gpt-5.5":
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
		case "gpt-5.8-slow":
			time.Sleep(200 * time.Millisecond)
			_, _ = io.WriteString(w, `{"id":"ok"}`)
		default:
			_, _ = io.WriteString(w, `{"id":"ok"}`)
		}
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-5.8-slow"},{"id":"gpt-5.4"},{"id":"gpt-6.1-terra"},{"id":"gpt-5.5"},{"id":"gpt-4o"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	result := VerifiedModels(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 2 * time.Second}, models, "gpt", "chat")
	if time.Since(start) > time.Second {
		t.Fatalf("probes took %s; they should run together", time.Since(start))
	}
	if !result.Check.OK || result.Check.Rejected() {
		t.Fatalf("check %+v", result.Check)
	}
	if strings.Join(result.Passed, ",") != "gpt-5.8-slow,gpt-5.4" {
		t.Fatalf("passed %v", result.Passed)
	}
	skipped := map[string]int{}
	for _, skip := range result.Skipped {
		skipped[skip.ID] = skip.Check.Status
	}
	if skipped["gpt-6.1-terra"] != http.StatusForbidden || skipped["gpt-5.5"] != http.StatusServiceUnavailable || len(result.Skipped) != 2 {
		t.Fatalf("skipped %+v", result.Skipped)
	}
}

func TestVerifiedModelsRejectsOnlyWhenNothingWorks(t *testing.T) {
	posts := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts++
		mu.Unlock()
		http.Error(w, `{"error":{"message":"quota"}}`, http.StatusForbidden)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := VerifiedModels(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: time.Second}, models, "gpt", "chat")
	if result.Check.OK || !result.Check.Rejected() || len(result.Passed) != 0 || len(result.Skipped) != 0 || posts != 2 {
		t.Fatalf("check %+v passed %v skipped %v posts %d", result.Check, result.Passed, result.Skipped, posts)
	}

	mixed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = readJSON(r, &body)
		if body.Model == "gpt-5.4" {
			http.Error(w, `{"error":{"message":"quota"}}`, http.StatusForbidden)
			return
		}
		http.Error(w, `{"error":{"message":"no channel"}}`, http.StatusServiceUnavailable)
	}))
	defer mixed.Close()
	ep, err = ParseBase(mixed.URL)
	if err != nil {
		t.Fatal(err)
	}
	result = VerifiedModels(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: time.Second}, models, "gpt", "chat")
	if result.Check.Rejected() || result.Check.OK || len(result.Passed) != 0 || len(result.Skipped) != 2 {
		t.Fatalf("mixed %+v passed %v skipped %v", result.Check, result.Passed, result.Skipped)
	}
}

func TestVerifiedModelsCapsTheCandidateList(t *testing.T) {
	var mu sync.Mutex
	got := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = readJSON(r, &body)
		mu.Lock()
		got[body.Model] = true
		mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	buf := `{"data":[`
	ids := make([]string, 9)
	for i := 1; i <= 9; i++ {
		id := "gpt-6." + string(rune('0'+i))
		ids[i-1] = id
		if i > 1 {
			buf += ","
		}
		buf += `{"id":"` + id + `"}`
	}
	buf += `]}`
	models, err := ParseModels([]byte(buf))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := VerifiedModels(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: time.Second}, models, "gpt", "chat")
	if len(result.Passed) != maxRecommendProbes || got["gpt-6.1"] || result.Passed[0] != "gpt-6.9" {
		t.Fatalf("passed %v probed %v", result.Passed, got)
	}
}

func TestTimeoutsDoNotRunOneAfterAnother(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = readJSON(r, &body)
		if body.Model == "gpt-5.4" {
			_, _ = io.WriteString(w, `{"id":"ok"}`)
			return
		}
		time.Sleep(2 * time.Second)
		_, _ = io.WriteString(w, `{"id":"slow"}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.5"},{"id":"gpt-5.6"},{"id":"gpt-5.7"},{"id":"gpt-5.8"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opt := ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 400 * time.Millisecond}
	start := time.Now()
	result := VerifiedModels(ctx, opt, models, "gpt", "chat")
	if time.Since(start) > time.Second {
		t.Fatalf("probes took %s; a timeout should not queue the rest", time.Since(start))
	}
	if strings.Join(result.Passed, ",") != "gpt-5.4" {
		t.Fatalf("passed %v", result.Passed)
	}
	if len(result.Skipped) != 4 {
		t.Fatalf("skipped %+v", result.Skipped)
	}
	for _, skip := range result.Skipped {
		if skip.Check.Status != 0 || skip.Check.OK {
			t.Fatalf("timeout skip %+v", skip)
		}
	}

	start = time.Now()
	check, skipped := FirstWorking(ctx, opt, models, "gpt", "chat")
	if time.Since(start) > time.Second {
		t.Fatalf("working check took %s", time.Since(start))
	}
	if !check.OK || check.Detail != "gpt-5.4" || len(skipped) != 4 {
		t.Fatalf("check %+v skipped %+v", check, skipped)
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
