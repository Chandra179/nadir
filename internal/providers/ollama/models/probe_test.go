package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckUsesConfiguredRoleModelWithoutInference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" || r.Method != http.MethodPost {
			t.Errorf("unexpected inference request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["model"] != "answer:4b" {
			t.Errorf("model request = %v / %v", body, err)
		}
		w.Write([]byte(`{"details":{"parameter_size":"4B"}}`))
	}))
	defer srv.Close()
	if err := NewDependencies(DependenciesConfig{Addr: srv.URL + "/", Model: "answer:4b"}).Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckMissingModelGivesInstallAction(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	err := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "missing:1b"}).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ollama pull missing:1b") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{"{}", "not JSON"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		err := NewDependencies(DependenciesConfig{Addr: srv.URL, Model: "answer"}).Check(context.Background())
		srv.Close()
		if err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
