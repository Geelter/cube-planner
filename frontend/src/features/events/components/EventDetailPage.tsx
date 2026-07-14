import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { UnauthorizedError, useEvent } from "../api";
import { formatFee } from "../lib/money";
import { EventStatusBadge } from "./EventsListPage";
import { RegistrationPanel } from "./RegistrationPanel";

const CONFIRM_POLL_MS = 2_000;
const CONFIRM_CAP_MS = 60_000;

export function EventDetailPage() {
  const { eventId } = useParams({ from: "/events/$eventId/" });
  const search = useSearch({ from: "/events/$eventId/" });
  const me = useMe();

  // "confirming…" after the Stripe redirect: poll until the webhook flips
  // the registration to paid, capped at 60s. State-driven so the hook
  // options stay a plain number | false.
  const [confirming, setConfirming] = useState(search.checkout === "success");
  const [timedOut, setTimedOut] = useState(false);
  const event = useEvent(eventId, { refetchInterval: confirming ? CONFIRM_POLL_MS : false });

  // Stop confirming once the server shows anything other than a pending
  // payment (paid = success; absent/expired = nothing left to confirm).
  useEffect(() => {
    if (!confirming || !event.data) return;
    if (event.data.myRegistration?.status !== "pending_payment") setConfirming(false);
  }, [confirming, event.data]);

  useEffect(() => {
    if (!confirming) return;
    const t = setTimeout(() => {
      setConfirming(false);
      setTimedOut(true);
    }, CONFIRM_CAP_MS);
    return () => clearTimeout(t);
  }, [confirming]);

  if (event.error instanceof UnauthorizedError) {
    return <p className="text-fg-muted">{event.error.message}</p>;
  }
  if (event.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (event.error)
    return (
      <p role="alert" className="text-danger">
        {m.events_not_found()}
      </p>
    );

  const e = event.data;
  const taken = e.paidCount + e.pendingCount;
  const isAdmin = me.data?.role === "admin";
  const cubes = e.cubes ?? [];
  const attendees = e.attendees ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4">
        <h1 className="flex items-center gap-2 text-2xl font-semibold text-fg">
          {e.name}
          <EventStatusBadge status={e.status} />
        </h1>
        {isAdmin && (
          <Button asChild variant="outline" size="sm">
            <Link to="/events/$eventId/manage" params={{ eventId }}>
              {m.event_manage()}
            </Link>
          </Button>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        <div>
          <dt className="text-fg-muted">{m.event_date()}</dt>
          <dd className="text-fg">{new Date(e.startsAt).toLocaleString(getLocale())}</dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_location()}</dt>
          <dd className="text-fg">{e.location || "—"}</dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_fee()}</dt>
          <dd className="text-fg">
            {e.feeCents === 0 ? m.events_free() : formatFee(e.feeCents, e.currency)}
          </dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_organizer()}</dt>
          <dd className="text-fg">{e.organizerName}</dd>
        </div>
      </dl>

      {e.description && <p className="whitespace-pre-wrap text-fg">{e.description}</p>}

      {confirming ? (
        <output className="block rounded-lg border border-border bg-surface-raised p-4 text-fg-muted">
          {m.event_confirming_payment()}
        </output>
      ) : (
        <>
          {timedOut && e.myRegistration?.status === "pending_payment" && (
            <p className="text-sm text-fg-muted">{m.event_confirming_timeout()}</p>
          )}
          <RegistrationPanel event={e} checkoutCancelled={search.checkout === "cancelled"} />
        </>
      )}

      {cubes.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.event_cubes()}</h2>
          <ul className="flex flex-col gap-1">
            {cubes.map((c) => (
              <li key={c.cubeId} className="text-sm">
                <Link
                  to="/cubes/$cubeId"
                  params={{ cubeId: c.cubeId }}
                  className="text-accent hover:underline"
                >
                  {c.cubeName}
                </Link>
                {c.pinnedVersion != null && c.pinnedAt && (
                  <span className="text-fg-muted">
                    {" "}
                    ·{" "}
                    {m.event_cube_pinned({
                      version: c.pinnedVersion,
                      date: new Date(c.pinnedAt).toLocaleDateString(getLocale()),
                    })}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="flex flex-col gap-2">
        <h2 className="text-lg font-medium text-fg">{m.event_attendees({ count: e.paidCount })}</h2>
        <p className="text-sm text-fg-muted">
          {m.events_spots({ taken, total: e.maxParticipants })}
          {e.waitlistCount > 0 && ` · ${m.events_waitlist_count({ count: e.waitlistCount })}`}
        </p>
        {attendees.length === 0 ? (
          <p className="text-sm text-fg-muted">{m.event_attendees_empty()}</p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {attendees.map((name, i) => (
              <li
                key={`${name}-${i}`}
                className="rounded-full border border-border px-3 py-1 text-sm text-fg"
              >
                {name}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
