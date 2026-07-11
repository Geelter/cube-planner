#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p frontend/src/shared/api
(cd backend && go run ./cmd/server openapi) > frontend/src/shared/api/openapi.yaml
pnpm --filter @cube-planner/frontend exec openapi-typescript src/shared/api/openapi.yaml -o src/shared/api/schema.d.ts
pnpm --filter @cube-planner/frontend exec oxfmt src/shared/api/schema.d.ts
