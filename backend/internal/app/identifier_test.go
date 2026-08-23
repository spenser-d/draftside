package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"draftside/internal/sleeper"
)

func TestParseDraftReference(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  DraftReference
	}{
		{"raw ID", "1385015915346657280", DraftReference{"1385015915346657280", IdentifierAmbiguous}},
		{"league URL", "https://sleeper.com/leagues/1385015915346657280/predraft", DraftReference{"1385015915346657280", IdentifierLeague}},
		{"scheme-less league URL", "sleeper.com/leagues/1385015915346657280/team", DraftReference{"1385015915346657280", IdentifierLeague}},
		{"draft URL", "https://sleeper.com/draft/nfl/1385015915359277056", DraftReference{"1385015915359277056", IdentifierDraft}},
		{"API league URL", "https://api.sleeper.app/v1/league/1385015915346657280", DraftReference{"1385015915346657280", IdentifierLeague}},
		{"API draft URL", "https://api.sleeper.app/v1/draft/1385015915359277056?ignored=true", DraftReference{"1385015915359277056", IdentifierDraft}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseDraftReference(test.input)
			if err != nil || got != test.want {
				t.Fatalf("ParseDraftReference(%q) = %+v, %v; want %+v", test.input, got, err, test.want)
			}
			if id := ExtractDraftID(test.input); id != test.want.ID {
				t.Fatalf("ExtractDraftID(%q) = %q; want %q", test.input, id, test.want.ID)
			}
		})
	}
}

func TestParseDraftReferenceRejectsUnsupportedInputs(t *testing.T) {
	inputs := []string{
		"",
		"1234",
		"not an id",
		"league 1385015915346657280",
		"https://example.com/leagues/1385015915346657280/predraft",
		"https://sleeper.com.evil.test/leagues/1385015915346657280/predraft",
		"https://sleeper.com/users/1385015915346657280",
		"https://sleeper.com/draft/nba/1385015915359277056",
	}
	for _, input := range inputs {
		if got, err := ParseDraftReference(input); err == nil {
			t.Errorf("ParseDraftReference(%q) unexpectedly returned %+v", input, got)
		}
	}
}

func TestResolveDraftPreservesDirectDraftBehavior(t *testing.T) {
	requests := []string{}
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		switch request.URL.Path {
		case "/draft/60001":
			writeResolutionJSON(t, response, sleeper.Draft{DraftID: "60001", Status: "pre_draft"})
		default:
			http.NotFound(response, request)
		}
	})

	for _, input := range []string{"60001", "https://sleeper.com/draft/nfl/60001"} {
		draft, err := service.resolveDraft(context.Background(), input)
		if err != nil || draft.DraftID != "60001" {
			t.Fatalf("resolveDraft(%q) = %+v, %v", input, draft, err)
		}
	}
	want := []string{"/draft/60001", "/draft/60001"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v; want %v", requests, want)
	}
}

func TestResolveRawIDFallsBackToLeagueOnlyOnDraftNotFound(t *testing.T) {
	requests := []string{}
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		switch request.URL.Path {
		case "/draft/70001":
			http.NotFound(response, request)
		case "/league/70001":
			writeResolutionJSON(t, response, sleeper.League{LeagueID: "70001", DraftID: "80001"})
		case "/draft/80001":
			writeResolutionJSON(t, response, sleeper.Draft{DraftID: "80001", LeagueID: "70001"})
		default:
			http.NotFound(response, request)
		}
	})
	draft, err := service.resolveDraft(context.Background(), "70001")
	if err != nil || draft.DraftID != "80001" {
		t.Fatalf("resolveDraft = %+v, %v", draft, err)
	}
	want := []string{"/draft/70001", "/league/70001", "/draft/80001"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v; want %v", requests, want)
	}
}

func TestResolveRawIDDoesNotMaskDraftServerError(t *testing.T) {
	requests := []string{}
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		response.WriteHeader(http.StatusInternalServerError)
	})
	_, err := service.resolveDraft(context.Background(), "70001")
	var apiErr *sleeper.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.Status != http.StatusInternalServerError {
		t.Fatalf("resolveDraft error = %v; want Sleeper 500", err)
	}
	if !reflect.DeepEqual(requests, []string{"/draft/70001"}) {
		t.Fatalf("requests = %v; league fallback must only happen on 404", requests)
	}
}

func TestResolveLeagueURLUsesLeaguePathAndCurrentDraft(t *testing.T) {
	requests := []string{}
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		switch request.URL.Path {
		case "/league/70001":
			writeResolutionJSON(t, response, sleeper.League{LeagueID: "70001", DraftID: "80001"})
		case "/draft/80001":
			writeResolutionJSON(t, response, sleeper.Draft{DraftID: "80001", LeagueID: "70001"})
		default:
			http.NotFound(response, request)
		}
	})
	draft, err := service.resolveDraft(context.Background(), "https://sleeper.com/leagues/70001/predraft")
	if err != nil || draft.DraftID != "80001" {
		t.Fatalf("resolveDraft = %+v, %v", draft, err)
	}
	want := []string{"/league/70001", "/draft/80001"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v; want %v", requests, want)
	}
}

func TestResolveLeagueFallsBackToMostRelevantDraft(t *testing.T) {
	requests := []string{}
	old, middle, newest := int64(10), int64(20), int64(30)
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		requests = append(requests, request.URL.Path)
		switch request.URL.Path {
		case "/league/70001":
			writeResolutionJSON(t, response, sleeper.League{LeagueID: "70001", DraftID: "99999"})
		case "/draft/99999":
			http.NotFound(response, request)
		case "/league/70001/drafts":
			writeResolutionJSON(t, response, []sleeper.Draft{
				{DraftID: "81001", Status: "complete", Season: "2026", StartTime: &newest},
				{DraftID: "81002", Status: "pre_draft", Season: "2025", StartTime: &middle},
				{DraftID: "81003", Status: "drafting", Season: "2024", StartTime: &old},
				{DraftID: "", Status: "drafting", Season: "2027"},
			})
		default:
			http.NotFound(response, request)
		}
	})
	draft, err := service.resolveDraft(context.Background(), "https://sleeper.com/leagues/70001/predraft")
	if err != nil || draft.DraftID != "81003" {
		t.Fatalf("resolveDraft = %+v, %v; want active draft 81003", draft, err)
	}
	want := []string{"/league/70001", "/draft/99999", "/league/70001/drafts"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %v; want %v", requests, want)
	}
}

func TestResolveLeagueWithoutDraftsIsNotFound(t *testing.T) {
	service := resolutionTestService(t, func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/league/70001":
			writeResolutionJSON(t, response, sleeper.League{LeagueID: "70001"})
		case "/league/70001/drafts":
			writeResolutionJSON(t, response, []sleeper.Draft{})
		default:
			http.NotFound(response, request)
		}
	})
	_, err := service.resolveDraft(context.Background(), "https://sleeper.com/leagues/70001/predraft")
	if !IsNotFound(err) {
		t.Fatalf("resolveDraft error = %v; want not found", err)
	}
}

func resolutionTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler(response, request)
		result := response.Result()
		if result.Body == nil {
			result.Body = io.NopCloser(strings.NewReader(""))
		}
		return result, nil
	})
	client := sleeper.NewClientWithHTTPClient("https://sleeper.test", &http.Client{Transport: transport})
	return NewService(client, nil, nil, 192)
}

func writeResolutionJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Fatalf("encode test response: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
