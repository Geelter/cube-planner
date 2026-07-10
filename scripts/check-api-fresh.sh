#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/gen-api.sh
if ! git diff --exit-code -- frontend/src/api; then
  echo "ERROR: generated API client is stale. Run 'pnpm gen:api' and commit." >&2
  exit 1
fi
