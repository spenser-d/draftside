// Package httpapi exposes the JSON API and serves the production SPA.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"draftside/internal/app"
)

type DraftService interface {
	Discover(ctx context.Context, username, directDraftID string) (app.Discovery, error)
	DraftView(ctx context.Context, draftID, userID, username string) (app.LiveDraftView, error)
}

type Server struct {
	service   DraftService
	staticDir string
	files     http.Handler
}

func New(service DraftService, staticDir string) http.Handler {
	if staticDir == "" {
		staticDir = "./web/dist"
	}
	server := &Server{service: service, staticDir: staticDir, files: http.FileServer(http.Dir(staticDir))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("POST /api/discover", server.discover)
	mux.HandleFunc("GET /api/draft", server.draft)
	mux.HandleFunc("/api/", func(response http.ResponseWriter, _ *http.Request) {
		writeError(response, http.StatusNotFound, "API route not found.")
	})
	mux.HandleFunc("/", server.spa)
	return recoverer(securityHeaders(mux))
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (server *Server) discover(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Username   string `json:"username"`
		DraftInput string `json:"draftInput"`
		DraftID    string `json:"draftId"`
	}
	decoder := json.NewDecoder(io.LimitReader(request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "Enter a valid discovery request.")
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" {
		writeError(response, http.StatusBadRequest, "Enter your Sleeper username.")
		return
	}
	draftInput := strings.TrimSpace(input.DraftInput)
	if draftInput == "" {
		draftInput = strings.TrimSpace(input.DraftID)
	}
	if draftInput != "" {
		if _, err := app.ParseDraftReference(draftInput); err != nil {
			writeError(response, http.StatusBadRequest, "That Sleeper league or draft ID or URL is not valid.")
			return
		}
	}
	discovery, err := server.service.Discover(request.Context(), input.Username, draftInput)
	if err != nil {
		server.serviceError(response, err, "Could not find that Sleeper account, league, or draft.")
		return
	}
	writeJSON(response, http.StatusOK, discovery)
}

func (server *Server) draft(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	draftID := strings.TrimSpace(query.Get("draftId"))
	userID := strings.TrimSpace(query.Get("userId"))
	username := strings.TrimSpace(query.Get("username"))
	if draftID == "" || userID == "" {
		writeError(response, http.StatusBadRequest, "Draft ID and user ID are required.")
		return
	}
	view, err := server.service.DraftView(request.Context(), draftID, userID, username)
	if err != nil {
		server.serviceError(response, err, "The live draft board is temporarily unavailable.")
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (server *Server) serviceError(response http.ResponseWriter, err error, fallback string) {
	status := http.StatusBadGateway
	if app.IsNotFound(err) {
		status = http.StatusNotFound
	}
	writeError(response, status, fallback)
}

func (server *Server) spa(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeError(response, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	requested := strings.TrimPrefix(filepath.Clean(request.URL.Path), string(filepath.Separator))
	if requested != "." && requested != "" {
		path := filepath.Join(server.staticDir, requested)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			server.files.ServeHTTP(response, request)
			return
		}
		if filepath.Ext(requested) != "" {
			http.NotFound(response, request)
			return
		}
	}
	indexPath := filepath.Join(server.staticDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		writeError(response, http.StatusServiceUnavailable, "Frontend build not found. Run npm run build:web first.")
		return
	}
	http.ServeFile(response, request, indexPath)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/healthz" {
			response.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				writeError(response, http.StatusInternalServerError, fmt.Sprintf("Unexpected server error: %v", recovered))
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"error": message})
}
