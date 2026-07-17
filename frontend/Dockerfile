# Keep in lockstep with .nvmrc. Do not bump past 24 blindly: node:25+
# images dropped corepack, which the pnpm setup below depends on.
FROM node:24-alpine AS build
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
