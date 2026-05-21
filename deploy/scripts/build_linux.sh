#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
BACKEND_DIR="${REPO_ROOT}/backend"
WEB_DIST="${REPO_ROOT}/apps/web/dist"
EMBED_DIST="${REPO_ROOT}/backend/internal/web/dist"
DIST_DIR="${REPO_ROOT}/dist/linux-amd64"

mkdir -p "${DIST_DIR}"

cd "${REPO_ROOT}"
corepack pnpm --filter @echorift/web build

rm -rf "${EMBED_DIST}"
mkdir -p "${EMBED_DIST}"
cp -R "${WEB_DIST}/." "${EMBED_DIST}/"

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go -C "${BACKEND_DIR}" build -trimpath -ldflags "-s -w" -o "${DIST_DIR}/echorift" ./cmd/echorift
go -C "${BACKEND_DIR}" build -trimpath -o "${DIST_DIR}/echorift-migrate" ./cmd/migrate
cp "${REPO_ROOT}/backend/.env.example" "${DIST_DIR}/echorift.env.example"
cp -R "${REPO_ROOT}/backend/migrations" "${DIST_DIR}/migrations"
cp "${REPO_ROOT}/deploy/systemd/echorift.service" "${DIST_DIR}/echorift.service"

tar -C "${DIST_DIR}/.." -czf "${REPO_ROOT}/dist/echorift-linux-amd64.tar.gz" linux-amd64

echo "Built ${REPO_ROOT}/dist/echorift-linux-amd64.tar.gz"
