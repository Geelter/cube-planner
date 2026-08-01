# Deploying to the VPS

How production works: every push to `master` that passes CI triggers the
Deploy workflow, which builds both images, pushes them to ghcr, and then
SSHes into the VPS to `docker compose pull && up -d` in
`/opt/cube-planner`. Caddy (baked into the web image) terminates TLS and
proxies `/api/*` and `/auth/oauth/*` to the API container; migrations
run automatically when the API boots.

Production has been live at https://cubeplanner.pl since 2026-08-01
(OAuth and Resend email included; Stripe is not configured yet, so paid
events 503 until the live keys land). The checklist below is kept as the
from-scratch runbook; "The production box" section after it documents
the machine as it actually stands.

## Go-live checklist

Everything needed for the full stack to run end-to-end, in order:

1. **VPS with Docker.** Install Docker Engine + the compose plugin.
   Open ports 80 and 443. Create a deploy user that can run `docker`.
2. **DNS.** Point A/AAAA records for both the apex domain and `www` at
   the VPS (`www` permanently redirects to the apex). Caddy provisions
   Let's Encrypt certificates automatically once the domains resolve —
   no manual TLS setup.
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

## The production box (as of 2026-08-01)

OVH VPS (4 vCPU / 8 GB / 75 GB NVMe), Debian 13, host `51.83.240.147`
(= what cubeplanner.pl resolves to). SSH as `debian` (passwordless
sudo); password auth is disabled
(`/etc/ssh/sshd_config.d/50-hardening.conf`), so access is key-only:
the GitHub Actions deploy key plus personal keys. To grant a new
machine access, generate a key on it and append its **public** half
from an already-authorized machine:

    ssh debian@51.83.240.147 'echo "ssh-ed25519 AAAA…" >> ~/.ssh/authorized_keys'

Installed beyond stock Debian:

- **unattended-upgrades** — security patches apply themselves.
- **Docker Engine + compose plugin** from Docker's own apt repo
  (`/etc/apt/sources.list.d/docker.list`); `debian` is in the `docker`
  group. Log rotation via `/etc/docker/daemon.json` (json-file,
  10 MB × 3 per container).
- **ghcr pull auth**: a `read:packages` PAT via `docker login`, stored
  in `~/.docker/config.json`.
- **Nightly DB backups**: systemd timer `cube-backup.timer` (03:00
  UTC, `Persistent=true`) runs `/opt/cube-planner/backup.sh` — a
  gzipped `pg_dump --clean --if-exists` into
  `/opt/cube-planner/backups/`, 14-day retention. Pull copies off-box
  now and then (only transfers dumps you don't already have locally):

      rsync -av debian@51.83.240.147:/opt/cube-planner/backups/ ~/cube-backups/

Everything app-shaped lives in `/opt/cube-planner`: the compose file,
`.env` (chmod 600, never touched by the pipeline), `backup.sh`, and
`backups/`. Three containers (`web` = Caddy on 80/443, `api`,
`postgres`) and two volumes (`caddy_data` = TLS certs, `pgdata` = the
database). Deploys only ever `pull && up -d && image prune` — the
system layer is hand-managed.

Day-to-day commands (from `/opt/cube-planner`):

    # what's running + health/restart state
    docker compose -f docker-compose.prod.yml ps
    # follow backend logs live (Ctrl-C to stop); --since 1h for a slice
    docker compose -f docker-compose.prod.yml logs -f api
    # SQL shell straight into the production database
    docker compose -f docker-compose.prod.yml exec postgres psql -U cube
    # when the next nightly backup fires + when the last one ran
    systemctl list-timers cube-backup.timer

**Changing `.env`:** edit it, then run
`docker compose -f docker-compose.prod.yml up -d`. Containers read env
only at creation, so compose recreates just the affected ones (a few
seconds of api downtime). Exception: `POSTGRES_PASSWORD` cannot be
rotated by editing alone — Postgres keeps the old password internally.

**Restoring a backup** (works on a dirty database or a brand-new box —
the dump carries `--clean`; on a fresh box bring up only `postgres`
first):

    # stop the backend so nothing writes mid-restore
    docker compose -f docker-compose.prod.yml stop api
    # decompress the dump and pipe it into psql inside the postgres
    # container; the dump's --clean statements drop + recreate every
    # table before loading, so no manual wipe is needed
    gunzip -c backups/cube-YYYY-MM-DD.sql.gz | \
      docker compose -f docker-compose.prod.yml exec -T postgres \
        psql -U cube -d cube
    # bring the backend back
    docker compose -f docker-compose.prod.yml start api

**Monitoring**: UptimeRobot pings the apex plus a keyword monitor
(`scryfallId`) on `/api/cards/search?name=bolt` — keyword type because
plain monitors probe with HEAD, which the GET-only API routes answer
with 405; and the apex alone stays green when only the api container
dies (Caddy still serves the SPA).

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

## Recovering a failed auto-refund

The events webhook handler auto-refunds two cases itself (duplicate
charges from a Pay race, and late payments that can't reclaim a spot).
If the Stripe API call fails, that failure is **not retryable**: the
webhook event was already marked seen and the handler still returns
success, so Stripe will not redeliver it. Recovery is manual — issue
the refund from the Stripe dashboard. Find the affected payment intent
in the API logs: grep for `"duplicate-charge auto-refund failed"` or
`"late-payment auto-refund failed"`, which log the `payment_intent` or
`registration` id.
