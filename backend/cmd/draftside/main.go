// Command draftside runs the web application and live-draft API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"draftside/internal/app"
	"draftside/internal/httpapi"
	"draftside/internal/live"
	"draftside/internal/sleeper"
	"draftside/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	rootContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := store.Open(environment("SQLITE_PATH", "./data/draftside.sqlite"))
	if err != nil {
		return err
	}
	defer database.Close()

	client := sleeper.NewClient(os.Getenv("SLEEPER_API_BASE_URL"))
	watchers := live.NewRegistry(rootContext, live.SleeperSource(client), milliseconds("SLEEPER_POLL_INTERVAL_MS", 2000))
	defer watchers.Close()
	service := app.NewService(client, watchers, database, integer("SIMULATION_SAMPLE_COUNT", 192))

	port := environment("PORT", "8080")
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(service, environment("STATIC_DIR", "./web/dist")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       12 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Draftside listening on http://localhost:%s", port)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func integer(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func milliseconds(key string, fallback int) time.Duration {
	return time.Duration(integer(key, fallback)) * time.Millisecond
}
