#!/bin/sh
# Starts Postgres and Redis on loopback, optionally a Cloudflare quick tunnel
# (PUBLIC=1), then runs the app in the foreground.
set -e

docker-entrypoint.sh postgres -c listen_addresses=127.0.0.1 >/tmp/postgres.log 2>&1 &
redis-server --bind 127.0.0.1 --save "" --appendonly no --daemonize yes >/dev/null

echo "waiting for postgres..."
until pg_isready -q -h 127.0.0.1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do sleep 0.5; done

if [ "${PUBLIC:-0}" = "1" ]; then
  cloudflared tunnel --no-autoupdate --protocol http2 --url http://127.0.0.1:8080 2>&1 |
    awk 'match($0, /https:\/\/[a-z0-9-]+\.trycloudflare\.com/) { print "PUBLIC URL: " substr($0, RSTART, RLENGTH); fflush() }' &
fi

echo "url-shortener on http://localhost:8080"
exec url-shortener
