# url-shortener

A URL shortener built with [GHP](https://github.com/GHP-GoLang-Framework/ghp) — PHP-style templates, compiled into real Go. Postgres is the source of truth; Redis is a cache-aside layer in front of it.

## Architecture

- **Postgres**: `links` table (`code`, `target_url`, `clicks`, `created_at`).
- **Redis**: cache-aside for `code -> target_url` lookups (1h TTL).
- `POST /` shortens a URL (random 7-char code, retried on collision) and redirects to `/?created=<code>`.
- `GET /{code}` resolves the code (Redis, falling back to Postgres on a miss), redirects (302) to the target, and bumps the click counter.

## Layout

- `index.ghp` — `/`: the shorten form (GET) and its submission handler (POST), plus a list of recently created links.
- `[code].ghp` — `/{code}`: resolves and redirects.
- `internal/store/store.go` — plain Go: Postgres (`pgxpool`) + Redis (`go-redis`), shared by every page through the package-level `db` var set up in `main.go`.

## Running it

```bash
docker compose up -d --build
```

Brings up Postgres (`:5432`), Redis (`:6379`) and the app (`:8080`). The app's Dockerfile builds the `ghp` CLI from the upstream repo and runs `ghp build` against this project — no local GHP checkout needed.

To run it outside Docker:

```bash
/path/to/ghp/bin/ghp build .   # transpiles *.ghp, generates ghproutes.go, compiles ./app
./app                           # listens on :8080 by default
```

Env vars (all optional):

- `DATABASE_URL` (default `postgres://ghp:ghp@localhost:5432/ghp_shortener?sslmode=disable`)
- `REDIS_ADDR` (default `localhost:6379`)
- `ADDR` (default `:8080`)
