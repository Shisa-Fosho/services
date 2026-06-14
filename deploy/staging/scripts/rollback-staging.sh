#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: rollback-staging.sh <previous-image-tag>" >&2
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

tmp_env=$(mktemp)
if grep -q '^IMAGE_TAG=' .env; then
  sed "s/^IMAGE_TAG=.*/IMAGE_TAG=$image_tag/" .env > "$tmp_env"
else
  cp .env "$tmp_env"
  printf '\nIMAGE_TAG=%s\n' "$image_tag" >> "$tmp_env"
fi
install -m 600 "$tmp_env" .env
rm -f "$tmp_env"

compose=(docker compose --env-file .env -f docker-compose.yml)

echo "Pulling app images for rollback tag $image_tag"
"${compose[@]}" pull trading platform settlement indexer

echo "Rolling app containers back to $image_tag"
"${compose[@]}" up -d --wait trading platform settlement indexer caddy

echo "Rollback complete. No automatic migrate down was run."
