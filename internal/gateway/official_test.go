package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckOfficialUsesVendorAuth(t *testing.T) {
	const key = "sk-official-test-key"
	cases := []struct {
		id   string
		path string
	}{
		{id: "openai", path: "/v1/models"},
		{id: "xai", path: "/v1/models"},
		{id: "gemini", path: "/v1beta/models"},
		{id: "anthropic", path: "/v1/models"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			var got http.Header
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				gotPath = r.URL.RequestURI()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"message":"bad `+key+`"}}`)
			}))
			defer srv.Close()
			check, err := CheckOfficial(context.Background(), tc.id, srv.URL, key, srv.Client())
			if err != nil {
				t.Fatal(err)
			}
			if gotPath != tc.path {
				t.Fatalf("path %s", gotPath)
			}
			if check.Status != http.StatusUnauthorized || !check.Rejected() || check.OK {
				t.Fatalf("status %d ok %v rejected %v", check.Status, check.OK, check.Rejected())
			}
			if strings.Contains(check.Excerpt, key) || strings.Contains(check.Message, key) || strings.Contains(check.Err, key) || strings.Contains(check.Hint, key) {
				t.Fatal("check leaked the token")
			}
			switch tc.id {
			case "anthropic":
				if got.Get("Authorization") != "" {
					t.Fatal("anthropic sent Authorization")
				}
				if got.Get("x-api-key") != key || got.Get("anthropic-version") != "2023-06-01" {
					t.Fatal("anthropic headers")
				}
			case "gemini":
				if got.Get("Authorization") != "" || got.Get("x-goog-api-key") != key {
					t.Fatal("gemini headers")
				}
			default:
				if got.Get("Authorization") != "Bearer "+key || got.Get("x-api-key") != "" {
					t.Fatal("bearer headers")
				}
			}
		})
	}
}

func TestCheckOfficialNetworkFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	check, err := CheckOfficial(context.Background(), "openai", url, "sk-official-test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if check.Status != 0 || check.OK || check.Rejected() {
		t.Fatalf("status %d ok %v", check.Status, check.OK)
	}
	if strings.Contains(check.Err, "sk-official-test-key") {
		t.Fatal("error leaked the token")
	}
}

func TestCheckOfficialAccepts200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()
	check, err := CheckOfficial(context.Background(), "anthropic", srv.URL, "sk-official-test-key", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !check.OK || check.Status != http.StatusOK {
		t.Fatalf("status %d ok %v", check.Status, check.OK)
	}
}
