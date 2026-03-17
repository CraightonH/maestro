#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

(
  cd "${ROOT}/web"
  npm run build
)

mkdir -p "${ROOT}/internal/api/static/app"
rsync -a --delete "${ROOT}/web/dist/" "${ROOT}/internal/api/static/app/"

echo "Synced web/dist into internal/api/static/app"
