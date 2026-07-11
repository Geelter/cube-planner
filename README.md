# Cube Planner

MTG cube management + events for a local community. See
`docs/superpowers/specs/2026-07-10-cube-planner-master-design.md`.

## Development

Requires Docker, Go, Node 22 + pnpm.

    make up        # Postgres + Mailpit (Docker), Go API, Vite dev server
    make help      # everything else

Mail sent by the app (verification, password reset) is captured by
Mailpit: http://localhost:8025.
