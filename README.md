# Draftside

Draftside is a local, read-only companion for Sleeper fantasy drafts. It watches the live board, identifies the user's next pick, simulates likely opponent selections, and presents one recommended player.

## Stack

- Vite + React for the browser interface
- Go in `backend/` for the API, shared live-draft watcher, recommendation engine, and production web server
- SQLite for draft snapshots and recommendation history

The production build is one Go program. It serves the compiled React app from `web/dist` and exposes the JSON API under `/api`.

## Run locally

Requirements: Go 1.24+ and Node.js 22+.

```sh
npm install
npm run dev
```

Open `http://localhost:5173`. Vite proxies API calls to the Go server on port 8080.

## Build and run as one program

```sh
npm run build
npm start
```

Open `http://localhost:8080`.

Copy `.env.example` values into your shell or process manager to change ports, the SQLite location, polling interval, simulation count, or static directory.

## Checks

```sh
npm test
npm run lint
```

Draftside never makes a draft selection. The final pick is still made in Sleeper.

## Connecting a draft

Enter a Sleeper username and optionally paste any of these into the connection field:

- a league URL
- a draft URL
- a raw league ID
- a raw draft ID

Leaving the field empty discovers the user's current Sleeper drafts. League links and IDs are resolved to the league's current draft while direct draft links and IDs continue to work.

Recommendations use the league's roster and scoring configuration when it is available. Mock drafts without league settings use a neutral scoring profile.
