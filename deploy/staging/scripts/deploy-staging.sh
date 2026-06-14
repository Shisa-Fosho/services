#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: deploy-staging.sh <immutable-image-tag>" >&2
  exit 2
fi

image_tag=$1
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

: "${STAGING_TAILSCALE_HOSTNAME:?STAGING_TAILSCALE_HOSTNAME is required}"
: "${STAGING_TAILSCALE_IP:?STAGING_TAILSCALE_IP is required}"
: "${IMAGE_PREFIX:?IMAGE_PREFIX is required}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed or not on PATH" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose plugin is not available" >&2
  exit 1
fi
if ! command -v tailscale >/dev/null 2>&1; then
  echo "tailscale is not installed or not on PATH" >&2
  exit 1
fi

if [ "$(id -un)" = "root" ]; then
  echo "Refusing to deploy as root; use the dedicated deploy user" >&2
  exit 1
fi

# Keep IMAGE_TAG in the runtime env aligned with the exact image SHA this deploy targets.
tmp_env=$(mktemp)
if grep -q '^IMAGE_TAG=' .env; then
  sed "s/^IMAGE_TAG=.*/IMAGE_TAG=$image_tag/" .env > "$tmp_env"
else
  cp .env "$tmp_env"
  printf '\nIMAGE_TAG=%s\n' "$image_tag" >> "$tmp_env"
fi
install -m 600 "$tmp_env" .env
rm -f "$tmp_env"
export IMAGE_TAG=$image_tag

install -d -m 700 certs
if ! tailscale cert \
  --cert-file certs/tailscale.crt \
  --key-file certs/tailscale.key \
  "$STAGING_TAILSCALE_HOSTNAME"; then
  cat >&2 <<MSG
Tailscale HTTPS certificate issuance failed for $STAGING_TAILSCALE_HOSTNAME.
Enable HTTPS/cert support for this Tailnet in the Tailscale admin console, then rerun this deploy.
MSG
  exit 42
fi
chmod 600 certs/tailscale.key
chmod 644 certs/tailscale.crt

compose=(docker compose --env-file .env -f docker-compose.yml)

echo "Pulling staging images for tag $image_tag"
"${compose[@]}" pull postgres nats prometheus loki tempo grafana caddy || true
"${compose[@]}" pull trading platform settlement indexer migrate

echo "Starting infrastructure containers"
"${compose[@]}" up -d --wait postgres nats prometheus loki tempo grafana

echo "Stopping app containers before migrations"
"${compose[@]}" stop trading platform settlement indexer || true

echo "Running migrations"
"${compose[@]}" run --rm migrate up

echo "Starting app and ingress containers"
"${compose[@]}" up -d --wait trading platform settlement indexer caddy

echo "Staging deploy complete for $image_tag"
