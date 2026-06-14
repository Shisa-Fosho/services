#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
staging_dir=$(cd -- "$script_dir/.." && pwd)
cd "$staging_dir"

if [ ! -f .env ]; then
  echo ".env is missing in $staging_dir" >&2
  exit 1
fi

set -a
# shellcheck source=/dev/null
. ./.env
set +a

: "${STAGING_CLOB_PUBLIC_URL:?STAGING_CLOB_PUBLIC_URL is required}"
: "${STAGING_PLATFORM_PUBLIC_URL:?STAGING_PLATFORM_PUBLIC_URL is required}"
: "${STAGING_POSTGRES_USER:?STAGING_POSTGRES_USER is required}"
: "${STAGING_POSTGRES_DB:?STAGING_POSTGRES_DB is required}"

compose=(docker compose --env-file .env -f docker-compose.yml)

echo "Docker Compose service state"
"${compose[@]}" ps

echo "Checking Postgres readiness"
"${compose[@]}" exec -T postgres pg_isready -U "$STAGING_POSTGRES_USER" -d "$STAGING_POSTGRES_DB"

echo "Checking NATS readiness"
"${compose[@]}" exec -T nats wget --no-verbose --tries=1 --spider http://localhost:8222/healthz

echo "Checking platform HTTPS health endpoint: $STAGING_PLATFORM_PUBLIC_URL/healthz"
curl --fail --show-error --silent --max-time 15 "$STAGING_PLATFORM_PUBLIC_URL/healthz"
printf '\n'

echo "Checking trading/CLOB HTTPS health endpoint: $STAGING_CLOB_PUBLIC_URL/healthz"
curl --fail --show-error --silent --max-time 15 "$STAGING_CLOB_PUBLIC_URL/healthz"
printf '\n'

echo "Recent app logs (last 50 lines each)"
"${compose[@]}" logs --tail=50 trading platform settlement indexer

echo "Smoke checks passed"
