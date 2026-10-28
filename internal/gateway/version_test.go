package gateway

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseModelVersion(t *testing.T) {
	cases := []struct {
		id   string
		want string
		ok   bool
	}{
		{"gemini-3.8-flash", "3.8", true},
		{"gemini-2.5-pro", "2.5", true},
		{"gemini-2.0-flash", "2.0", true},
		{"gemini-2.5-flash-preview-09-2025", "2.5", true},
		{"claude-sonnet-4-6", "4.6", true},
		{"claude-opus-4-5", "4.5", true},
		{"claude-fable-5", "5", true},
		{"claude-3-5-sonnet-20241022", "3.5", true},
		{"gpt-5.4-mini", "5.4", true},
		{"gpt-5.4.1", "5.4.1", true},
		{"gpt-5.10", "5.10", true},
		{"gpt-5.40", "5.40", true},
		{"gpt-6", "6", true},
		{"gpt-6.1-terra", "6.1", true},
		{"GPT-5.4", "5.4", true},
		{"gpt-5", "5", true},
		{"gpt-5.0", "5.0", true},
		{"gpt-5.3-mini", "5.3", true},
		{"gpt-4.1", "4.1", true},
		{"gpt-4.1-mini", "4.1", true},
		{"gpt-4o", "4", true},
		{"gpt-4o-mini", "4", true},
		{"gpt-3.5-turbo", "3.5", true},
		{"chatgpt-4o-latest", "4", true},
		{"gpt-slow", "", false},
		{"gpt-image-keep", "", false},
		{"gpt-oss-120b", "", false},
		{"grok-4", "4", true},
		{"o3", "3", true},
	}
	for _, tc := range cases {
		got, ok := parseModelVersion(tc.id)
		if ok != tc.ok || joinInts(got) != tc.want {
			t.Errorf("%s: got %s ok=%v, want %s ok=%v", tc.id, joinInts(got), ok, tc.want, tc.ok)
		}
	}
}

func TestGPTFloor(t *testing.T) {
	eligible := []string{
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.4.1", "gpt-5.10", "gpt-5.40", "gpt-6", "gpt-6.1-terra", "GPT-5.4",
	}
	for _, id := range eligible {
		if gptBelowFloor(id) {
			t.Errorf("%s should stay eligible", id)
		}
	}
	excluded := []string{
		"gpt-5", "gpt-5.0", "gpt-5.3", "gpt-5.3-mini", "gpt-4.1", "gpt-4.1-mini",
		"gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo", "chatgpt-4o-latest",
		"gpt-slow", "gpt-image-keep", "gpt-oss-120b",
	}
	for _, id := range excluded {
		if !gptBelowFloor(id) {
			t.Errorf("%s should be excluded", id)
		}
	}
	for _, id := range []string{"o3", "gemini-2.5-pro", "claude-sonnet-4-6", "grok-4"} {
		if gptBelowFloor(id) {
			t.Errorf("%s is not a GPT model", id)
		}
	}
}

func TestNewestFirstIgnoresListOrderAndRecommendation(t *testing.T) {
	models := []Model{
		{ID: "gemini-2.5-pro", Recommended: true},
		{ID: "gemini-2.0-flash"},
		{ID: "gemini-2.5-flash-preview-09-2025"},
		{ID: "gemini-3.8-flash"},
	}
	got := strings.Join(CandidateOrder(models, "gemini"), ",")
	want := "gemini-3.8-flash,gemini-2.5-pro,gemini-2.5-flash-preview-09-2025,gemini-2.0-flash"
	if got != want {
		t.Fatalf("order %s", got)
	}
	if PickDefaultModels(models, "gemini") != "gemini-3.8-flash" {
		t.Fatalf("default %s", PickDefaultModels(models, "gemini"))
	}

	claude := []Model{
		{ID: "claude-opus-4-6"},
		{ID: "claude-sonnet-4-6"},
		{ID: "claude-opus-4-5"},
		{ID: "claude-fable-5"},
		{ID: "claude-sonnet-4-5", Recommended: true},
	}
	got = strings.Join(CandidateOrder(claude, "claude"), ",")
	want = "claude-fable-5,claude-sonnet-4-6,claude-opus-4-6,claude-sonnet-4-5,claude-opus-4-5"
	if got != want {
		t.Fatalf("claude order %s", got)
	}
}

func TestFamilyIDsDropsGPTBelowFloor(t *testing.T) {
	models := []Model{
		{ID: "gpt-4.1"},
		{ID: "gpt-4o"},
		{ID: "gpt-5.3-mini"},
		{ID: "gpt-5"},
		{ID: "gpt-5.4-mini"},
		{ID: "gpt-5.4"},
		{ID: "gpt-6"},
		{ID: "claude-sonnet-4-6"},
	}
	got := strings.Join(FamilyIDs(models, "gpt"), ",")
	if got != "gpt-6,gpt-5.4-mini,gpt-5.4" {
		t.Fatalf("family %s", got)
	}
	if PickDefaultModels(models, "gpt") != "gpt-6" {
		t.Fatalf("default %s", PickDefaultModels(models, "gpt"))
	}
	if FamilyIDs([]Model{{ID: "gpt-4.1"}, {ID: "gpt-4o"}}, "gpt") != nil {
		t.Fatal("only old gpt models should leave no candidate")
	}
}

func joinInts(v []int) string {
	if len(v) == 0 {
		return ""
	}
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}
