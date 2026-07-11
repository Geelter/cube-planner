FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /repo
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY frontend/package.json frontend/
RUN pnpm install --frozen-lockfile --filter @cube-planner/frontend --ignore-scripts
COPY frontend frontend
RUN pnpm --filter @cube-planner/frontend build

FROM caddy:2-alpine
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /repo/frontend/dist /srv
