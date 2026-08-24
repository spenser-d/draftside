# AGENTS.md

## Cursor Cloud specific instructions

Draftside is a read-only Sleeper fantasy-draft companion. It is a single product with two dev
processes: a Vite + React frontend (port `5173`) and a Go API (port `8080`). Vite proxies `/api`
and `/healthz` to the Go server. Standard commands live in `README.md` and `package.json` scripts.

### Toolchain

- The backend requires Go 1.24+ (`backend/go.mod` declares `go 1.24.0`). The base image's default
  `/usr/bin/go` is older, so Go 1.24 is installed at `/usr/local/go-1.24` and symlinked into
  `/usr/local/bin` (which precedes `/usr/bin` on `PATH`). `go version` should report 1.24.x. If it
  reports an older version, re-check that the `/usr/local/bin/go` symlink still exists.
- Node 22 (`>=22.13.0`) is already present. The SQLite driver is pure Go (`modernc.org/sqlite`),
  so no C compiler / CGO is needed.

### Running

- `npm run dev` runs the API and web dev server together via `concurrently`. Run it as a
  long-lived process (e.g. a tmux terminal), not in `install`/`start`. Open `http://localhost:5173`.
- The app calls the live Sleeper API (`https://api.sleeper.app/v1`). Discovery needs a real Sleeper
  username; off-season, accounts usually have no active drafts, so "Find my draft" legitimately
  returns "No current drafts were found." Use **"Explore a sample live room"** on the connect screen
  to exercise the full live-draft UI (recommendation, roster context, recent picks) without a live draft.
- Production single-binary mode (`npm run build && npm start`, port `8080`) requires `web/dist` to
  exist first (`npm run build:web`), otherwise the server returns "Frontend build not found."

### Checks

- `npm run lint` = ESLint + `go vet ./...`; `npm test` = `tsc --noEmit` + `go test ./...`.
