#!/bin/sh
# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: AGPL-3.0-only

# Runs specs against Postgres 18 instead of SQLite. Everything runs in docker,
# on one network: Postgres in its own container, and the server, vite and
# Playwright in a second one, so no port has to be published to the host.
#
#   qa/postgres.sh tests/teams.spec.js tests/teams-roles.spec.js
#
# Each run starts on a fresh database. Postgres is left running for the next
# run; `docker rm -f qa-pg && docker network rm qa-pg` removes it.
set -eu
repo=$(cd "$(dirname "$0")/.." && pwd)
pg=${QA_PG_CONTAINER:-qa-pg}
net=${QA_PG_NETWORK:-qa-pg}
playwright=$(cd "$repo/qa" && node -p "require('@playwright/test/package.json').version")

docker network inspect "$net" >/dev/null 2>&1 || docker network create "$net" >/dev/null
if ! docker container inspect "$pg" >/dev/null 2>&1; then
  docker run -d --name "$pg" --network "$net" -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=agentrq postgres:18-alpine >/dev/null
  until docker exec "$pg" pg_isready -U postgres >/dev/null 2>&1; do sleep 1; done
fi
docker exec "$pg" psql -U postgres -qc 'DROP DATABASE IF EXISTS agentrq WITH (FORCE)' -c 'CREATE DATABASE agentrq'

# A static server for the container's Linux, on this machine's architecture.
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
(cd "$repo/backend/cmd/server" && CGO_ENABLED=0 GOOS=linux go build -o "$out/agentrq-server" .)

tar c -C "$repo" --exclude=frontend/dist --exclude=qa/test-results frontend qa -C "$out" agentrq-server |
  docker run -i --rm --network "$net" --ipc=host \
    -e CI=1 -e QA_POSTGRES_HOST="$pg" -e QA_BACKEND_CMD=/src/agentrq-server -e QA_SERVER_LOGS="${QA_SERVER_LOGS:-}" \
    "mcr.microsoft.com/playwright:v$playwright-noble" sh -c "
      (apt-get update -qq && apt-get install -y -qq postgresql-client) >/dev/null 2>&1 || { echo 'installing psql failed'; exit 1; }
      mkdir -p /src/backend/cmd/server && tar x -C /src && cd /src/qa && npx playwright test --reporter=list $*"
