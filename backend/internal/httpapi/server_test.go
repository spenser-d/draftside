package httpapi

// HTTP contract tests use an in-memory service and server.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"draftside/internal/app"
)

type fakeService struct {
	lastDiscoveryInput string
}

func (service *fakeService) Discover(_ context.Context, username, draftInput string) (app.Discovery, error) {
	service.lastDiscoveryInput = draftInput
	reference, _ := app.ParseDraftReference(draftInput)
	return app.Discovery{UserID: "user-1", DisplayName: username, Candidates: []app.DraftCandidate{{DraftID: reference.ID}}}, nil
}

func (*fakeService) DraftView(_ context.Context, _, _, _ string) (app.LiveDraftView, error) {
	return app.LiveDraftView{}, nil
}

func TestHealthAndDiscovery(t *testing.T) {
	service := &fakeService{}
	handler := New(service, t.TempDir())
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health response: %d %s", health.Code, health.Body.String())
	}

	discovery := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"drafter","draftId":"https://sleeper.com/draft/nfl/123456"}`)
	handler.ServeHTTP(discovery, httptest.NewRequest(http.MethodPost, "/api/discover", body))
	var result app.Discovery
	if err := json.NewDecoder(discovery.Body).Decode(&result); err != nil || discovery.Code != http.StatusOK || result.Candidates[0].DraftID != "123456" {
		t.Fatalf("discovery: status=%d result=%+v err=%v", discovery.Code, result, err)
	}
	if service.lastDiscoveryInput != "https://sleeper.com/draft/nfl/123456" {
		t.Fatalf("service input = %q; URL classification must be preserved", service.lastDiscoveryInput)
	}
}

func TestDiscoveryAcceptsLeagueInputAndPrefersNewField(t *testing.T) {
	service := &fakeService{}
	handler := New(service, t.TempDir())
	response := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"drafter","draftInput":"https://sleeper.com/leagues/70001/predraft","draftId":"60001"}`)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/discover", body))
	if response.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", response.Code, response.Body.String())
	}
	if service.lastDiscoveryInput != "https://sleeper.com/leagues/70001/predraft" {
		t.Fatalf("service input = %q; draftInput should take precedence", service.lastDiscoveryInput)
	}
}

func TestDiscoveryRejectsUnsupportedIdentifierURL(t *testing.T) {
	service := &fakeService{}
	handler := New(service, t.TempDir())
	response := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"drafter","draftInput":"https://example.com/leagues/70001"}`)
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/discover", body))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("discovery: %d %s", response.Code, response.Body.String())
	}
	if service.lastDiscoveryInput != "" {
		t.Fatalf("service should not receive invalid input, got %q", service.lastDiscoveryInput)
	}
}

func TestSPAFallback(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<h1>Draftside</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(&fakeService{}, staticDir)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/draft/123", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Draftside") {
		t.Fatalf("SPA response: %d %s", response.Code, response.Body.String())
	}
}
