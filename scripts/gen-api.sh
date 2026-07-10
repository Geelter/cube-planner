#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p frontend/src/api
(cd backend && go run ./cmd/server openapi) > frontend/src/api/openapi.yaml
pnpm --filter @cube-planner/frontend exec openapi-typescript src/api/openapi.yaml -o src/api/schema.d.ts
pnpm --filter @cube-planner/frontend exec oxfmt src/api/schema.d.ts
