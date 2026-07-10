# Cube Planner

MTG cube management + events for a local community. See
`docs/superpowers/specs/2026-07-10-cube-planner-master-design.md`.

## Dev quickstart

- `pnpm install`
- `docker compose -f deploy/docker-compose.dev.yml up -d` (Postgres)
- `cd backend && DATABASE_URL=postgres://cube:cube@localhost:5432/cube?sslmode=disable go run ./cmd/server`
- `pnpm dev` (Vite on :5173, proxies /api to :8080)
