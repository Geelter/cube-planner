# Deploying to the VPS

How production works: every push to `master` that passes CI triggers the
Deploy workflow, which builds both images, pushes them to ghcr, and then
SSHes into the VPS to `docker compose pull && up -d` in
`/opt/cube-planner`. Caddy (baked into the web image) terminates TLS and
proxies `/api/*` and `/auth/oauth/*` to the API container; migrations
run automatically when the API boots.

As of 2026-07-17 the images build and push to ghcr successfully, but the
deploy job stops at the SSH step because nothing below is set up yet.

## Go-live checklist

Everything still needed for the full stack to run end-to-end, in order:

1. **VPS with Docker.** Install Docker Engine + the compose plugin.
   Open ports 80 and 443. Create a deploy user that can run `docker`.
2. **DNS.** Point an A/AAAA record for your domain at the VPS. Caddy
   provisions Let's Encrypt certificates automatically once the domain
   resolves — no manual TLS setup.
3. **Files on the VPS** (placed manually, not managed by the pipeline):
   - `/opt/cube-planner/docker-compose.prod.yml` — copy from this
     directory.
   - `/opt/cube-planner/.env` — copy `.env.example` and fill it in.
     Note `GHCR_OWNER=geelter` must stay lowercase (ghcr requirement).
4. **ghcr image access.** The images are private by default. Either
   make the `cube-planner-api` and `cube-planner-web` packages public
   (GitHub → Packages → package settings), or `docker login ghcr.io` on
   the VPS with a personal access token that has `read:packages`.
5. **GitHub Actions secrets** (repo → Settings → Secrets → Actions):
   `VPS_HOST`, `VPS_USER`, and `VPS_SSH_KEY` (private key whose public
   half is in the deploy user's `authorized_keys`).
6. **Real SMTP relay.** Production sends verification, password-reset,
   and event emails; fill the `SMTP_*` vars with a real provider
   (Mailpit is dev-only). The client uses opportunistic STARTTLS and
   only authenticates when `SMTP_USER` is set.
7. **OAuth apps** (optional — email/password login works without them).
   Create production Discord/Google apps with redirect URIs
   `https://<domain>/auth/oauth/discord/callback` and
   `https://<domain>/auth/oauth/google/callback`, then fill the
   `*_CLIENT_ID`/`*_CLIENT_SECRET` vars. Providers with empty
   credentials are simply disabled.
8. **Stripe live mode** (optional — leave both keys empty to run
   free-events-only; paid events return 503 `payments-unconfigured`).
   Fill `STRIPE_SECRET_KEY` with the live secret key and register a
   webhook endpoint in the Stripe dashboard for
   `https://<domain>/api/stripe/webhook` (events:
   `checkout.session.completed`, `checkout.session.expired`,
   `charge.refunded`); put its signing secret in
   `STRIPE_WEBHOOK_SECRET`. Setting exactly one of the two is a fatal
   startup error by design.

## First boot

- Migrations run automatically; the card mirror then imports ~97k
  printings from Scryfall (a ~450MB download, takes a few minutes) and
  refreshes every 6 hours.
- Grant yourself the organizer role manually:
  `docker compose -f docker-compose.prod.yml exec postgres psql -U cube
  -c "update users set role = 'admin' where email = '<you>';"`
- Deploys can also be triggered by hand: Actions → Deploy → Run
  workflow.

## Smoke test

Register an account (verification email arrives → SMTP works), log in,
search a card on `/cards` (mirror imported), create a free event and
register for it. If Stripe is configured, run one paid registration and
confirm the registration flips to paid (webhook works).
