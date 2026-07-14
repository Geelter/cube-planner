import { Link } from "@tanstack/react-router";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { useEvents, UnauthorizedError, type EventSummary } from "../api";
import { formatFee } from "../lib/money";

export function EventStatusBadge({ status }: { status: EventSummary["status"] }) {
  const label = {
    draft: m.events_status_draft(),
    published: m.events_status_published(),
    started: m.events_status_started(),
    finished: m.events_status_finished(),
    cancelled: m.events_status_cancelled(),
  }[status];
  return (
    <span className="rounded-full border border-border px-2 py-0.5 text-xs text-fg-muted">
      {label}
    </span>
  );
}

export function MyStatusBadge({
  status,
  pos,
}: {
  status?: string | null | undefined;
  pos?: number | null;
}) {
  if (!status) return null;
  const label =
    status === "pending_payment"
      ? m.events_my_pending()
      : status === "paid"
        ? m.events_my_paid()
        : status === "waitlisted"
          ? pos != null
            ? m.events_my_waitlisted({ pos })
            : m.events_my_waitlisted_nopos()
          : status === "refund_requested"
            ? m.events_my_refund_requested()
            : null;
  if (!label) return null;
  return (
    <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs font-medium text-accent">
      {label}
    </span>
  );
}

function EventRow({ event }: { event: EventSummary }) {
  const taken = event.paidCount + event.pendingCount;
  return (
    <li>
      <Link
        to="/events/$eventId"
        params={{ eventId: event.id }}
        className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-surface-raised p-4 hover:border-accent"
      >
        <div className="flex flex-col gap-1">
          <span className="flex items-center gap-2 font-medium text-fg">
            {event.name}
            <EventStatusBadge status={event.status} />
            <MyStatusBadge status={event.myRegistrationStatus} />
          </span>
          <span className="text-sm text-fg-muted">
            {new Date(event.startsAt).toLocaleString(getLocale())}
            {event.location && ` · ${event.location}`}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1 text-sm">
          <span className="text-fg">
            {event.feeCents === 0 ? m.events_free() : formatFee(event.feeCents, event.currency)}
          </span>
          <span className="text-fg-muted">
            {m.events_spots({ taken, total: event.maxParticipants })}
            {event.waitlistCount > 0 &&
              ` · ${m.events_waitlist_count({ count: event.waitlistCount })}`}
          </span>
        </div>
      </Link>
    </li>
  );
}

export function EventsListPage() {
  const me = useMe();
  const events = useEvents();

  if (events.error instanceof UnauthorizedError) {
    return <p className="text-fg-muted">{events.error.message}</p>;
  }
  if (events.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (events.error)
    return (
      <p role="alert" className="text-danger">
        {events.error.message}
      </p>
    );

  const drafts = events.data.filter((e) => e.status === "draft");
  const upcoming = events.data.filter((e) => e.status === "published" || e.status === "started");
  const past = events.data.filter((e) => e.status === "finished" || e.status === "cancelled");
  const isAdmin = me.data?.role === "admin";
  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-fg">{m.events_title()}</h1>
        {isAdmin && (
          <Button asChild size="sm">
            <Link to="/events/new">{m.events_new_button()}</Link>
          </Button>
        )}
      </div>
      {events.data.length === 0 && <p className="text-fg-muted">{m.events_empty()}</p>}
      {isAdmin && drafts.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_drafts()}</h2>
          <ul className="flex flex-col gap-2">
            {drafts.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </ul>
        </section>
      )}
      {upcoming.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_upcoming()}</h2>
          <ul className="flex flex-col gap-2">
            {upcoming.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </ul>
        </section>
      )}
      {past.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_past()}</h2>
          <ul className="flex flex-col gap-2">
            {past.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
