# Shisa services staging

Private staging runs on the Hermes VPS with Docker Compose and is deployed automatically from the `staging` branch.

## Endpoint shape

Frontend-facing endpoints are HTTPS over Tailscale only:

- Trading / CLOB: `https://hermes-clam-server.taileedc49.ts.net:8443`
- Platform: `https://hermes-clam-server.taileedc49.ts.net:8444`

No application, database, NATS, gRPC, metrics, or observability ports are intentionally exposed on the public interface. Docker publishes only the Caddy HTTPS ports bound to the VPS Tailscale IP.

## Automation model

- Pull requests targeting `staging` run validation and image build checks only.
- Pushes/merges to `staging` validate, build/push GHCR images, copy the exact commit's deploy artifacts to `/opt/shisa/staging`, materialize `/opt/shisa/staging/.env` from GitHub Environment Secrets, deploy, migrate, and smoke-check.
- Runtime secrets live in GitHub Environment `staging`; this directory contains placeholders only.

## Required GitHub Environment Secrets

Deploy access:

```text
STAGING_DEPLOY_HOST
STAGING_DEPLOY_PORT
STAGING_DEPLOY_USER
STAGING_DEPLOY_SSH_KEY
```

Runtime config:

```text
STAGING_POSTGRES_USER
STAGING_POSTGRES_PASSWORD
STAGING_POSTGRES_DB
STAGING_JWT_ACCESS_SECRET
STAGING_JWT_REFRESH_SECRET
STAGING_APIKEY_DERIVATION_SECRET
STAGING_APIKEY_ENCRYPTION_KEY
STAGING_POLYGON_RPC_URL
STAGING_CONDITIONAL_TOKENS_ADDRESS
STAGING_NEG_RISK_ADAPTER_ADDRESS
STAGING_COLLATERAL_TOKEN_ADDRESS
STAGING_TAILSCALE_HOSTNAME
STAGING_TAILSCALE_IP
STAGING_CLOB_PUBLIC_URL
STAGING_PLATFORM_PUBLIC_URL
STAGING_GRAFANA_ADMIN_USER
STAGING_GRAFANA_ADMIN_PASSWORD
```

## VPS prerequisites

- Docker Engine and Docker Compose plugin installed.
- Dedicated deploy user can run Docker Compose non-interactively.
- Deploy user owns `/opt/shisa/staging`.
- Tailscale is online.
- Tailscale HTTPS/cert support is enabled for the Tailnet so `tailscale cert <hostname>` works on the VPS.

## Deploy flow

The GitHub Actions workflow runs:

```text
checkout exact staging commit
validate + build/push images
copy deploy/staging artifacts to /opt/shisa/staging
write locked-down .env from Environment Secrets
ssh to deploy user
scripts/deploy-staging.sh <commit-sha>
scripts/smoke-staging.sh
```

`deploy-staging.sh` stops DB-touching app containers before migrations, keeps Postgres/NATS running, runs `migrate up`, then starts app and ingress containers.

## Manual smoke check

From the VPS as the deploy user:

```bash
cd /opt/shisa/staging
./scripts/smoke-staging.sh
```

## Rollback

Rollback is app-image rollback only:

```bash
cd /opt/shisa/staging
./scripts/rollback-staging.sh <previous-known-good-sha>
./scripts/smoke-staging.sh
```

The rollback script does **not** run `migrate down`. Current migration rollback semantics are too broad for safe automatic deploy rollback.
