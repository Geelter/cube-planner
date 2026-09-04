import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import type { EventRegistrationRow, EventSummary } from "../api";
import {
  useDenyRefund,
  useEventRegistrations,
  useRefundRegistration,
  useRemoveRegistration,
} from "../api";

type EventStatus = EventSummary["status"];

const ALL_STATUSES: EventStatus[] = ["draft", "published", "started", "finished", "cancelled"];

// Two groups outlive `published`:
//
// - `queue`, because a player who self-cancels past the refund deadline lands
//   in refund_requested and the organizer still needs a screen to resolve it.
// - `paid`, on `started` only, because a no-show is by definition discovered
//   after the event starts — without it the remove action is unreachable in
//   exactly the case it exists for. It renders roster-style there: the Refund
//   button is suppressed (see `canRefund`), leaving only Remove.
//
// Outside `published` a group renders only when it has rows, so the common
// case is still a clean page.
const GROUPS: {
  key: string;
  title: () => string;
  statuses: string[];
  visibleOn: EventStatus[];
}[] = [
  {
    key: "paid",
    title: () => m.regs_group_paid(),
    statuses: ["paid"],
    visibleOn: ["published", "started"],
  },
  {
    key: "pending",
    title: () => m.regs_group_pending(),
    statuses: ["pending_payment"],
    visibleOn: ["published"],
  },
  {
    key: "waitlist",
    title: () => m.regs_group_waitlist(),
    statuses: ["waitlisted"],
    visibleOn: ["published"],
  },
  {
    key: "queue",
    title: () => m.regs_group_refund_queue(),
    statuses: ["refund_requested"],
    visibleOn: ALL_STATUSES,
  },
  {
    key: "history",
    title: () => m.regs_group_history(),
    statuses: ["cancelled", "refunded", "expired", "removed"],
    visibleOn: ["published"],
  },
];

type Confirm =
  | { kind: "refund" | "deny"; row: EventRegistrationRow }
  | { kind: "remove"; row: EventRegistrationRow; keepPayment: boolean };

export function RegistrationsTable({
  eventId,
  status,
}: {
  eventId: string;
  status: EventSummary["status"];
}) {
  const regs = useEventRegistrations(eventId);
  const refund = useRefundRegistration(eventId);
  const deny = useDenyRefund(eventId);
  const remove = useRemoveRegistration(eventId);
  const [confirm, setConfirm] = useState<Confirm | null>(null);

  if (regs.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (regs.error)
    return (
      <p role="alert" className="text-danger">
        {regs.error.message}
      </p>
    );

  const rowsFor = (statuses: string[]) =>
    (regs.data ?? [])
      .filter((r) => statuses.includes(r.status))
      .sort((a, b) => (a.waitlistPos ?? 0) - (b.waitlistPos ?? 0));

  const published = status === "published";
  const visible = GROUPS.filter(
    (g) => g.visibleOn.includes(status) && (published || rowsFor(g.statuses).length > 0),
  );
  if (visible.length === 0) return null;

  // Refunding a paid row is a `published` action: once the event has started
  // the organizer's tool is Remove (keep or refund is then a Stripe-dashboard
  // decision). Rows in the refund queue keep their buttons at every status —
  // that queue is the whole reason the section outlives `published`.
  const canRefund = (r: EventRegistrationRow) =>
    r.status === "refund_requested" || (r.status === "paid" && r.hasPayment && published);

  const confirmMessage = () => {
    if (confirm == null) return "";
    const name = confirm.row.displayName;
    if (confirm.kind !== "remove") {
      return confirm.kind === "deny"
        ? m.regs_deny_confirm({ name })
        : m.regs_refund_confirm({ name });
    }
    const base = confirm.keepPayment
      ? m.regs_remove_keep_confirm({ name })
      : m.regs_remove_confirm({ name });
    // Once the event has started, removing also drops the player from the
    // pairings. Say so here rather than let the organizer find out in the
    // next round.
    return status === "started" ? `${base} ${m.regs_remove_drops_from_tournament()}` : base;
  };

  const err = refund.error ?? deny.error ?? remove.error;
  const locale = getLocale();
  const refundingId = refund.isPending ? refund.variables : null;
  const denyingId = deny.isPending ? deny.variables : null;
  const removingId = remove.isPending ? remove.variables.registrationId : null;

  const rowMeta = (r: EventRegistrationRow) => {
    if (r.status === "pending_payment" && r.expiresAt) {
      return m.regs_expires({ date: new Date(r.expiresAt).toLocaleString(locale) });
    }
    if (r.status === "paid" && r.paidAt) {
      return m.regs_paid_at({ date: new Date(r.paidAt).toLocaleString(locale) });
    }
    if (r.status === "waitlisted" && r.waitlistPos != null) return `#${r.waitlistPos}`;
    const historyLabels: Partial<Record<EventRegistrationRow["status"], string>> = {
      cancelled: m.regs_status_cancelled(),
      refunded: m.regs_status_refunded(),
      expired: m.regs_status_expired(),
      removed: m.regs_status_removed(),
    };
    return historyLabels[r.status] ?? r.status;
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.regs_title()}</h2>
      {err && (
        <p role="alert" className="text-sm text-danger">
          {err.message}
        </p>
      )}
      {visible.map((g) => {
        const rows = rowsFor(g.statuses);
        return (
          <div key={g.key} className="flex flex-col gap-1">
            <h3 className="text-sm font-medium text-fg-muted">{g.title()}</h3>
            {rows.length === 0 ? (
              <p className="text-sm text-fg-muted">{m.regs_empty()}</p>
            ) : (
              <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
                {rows.map((r) => (
                  <li
                    key={r.id}
                    className="flex flex-wrap items-center justify-between gap-2 p-3 text-sm"
                  >
                    <span className="text-fg">
                      {r.displayName} <span className="text-fg-muted">({r.email})</span>
                    </span>
                    <span className="flex items-center gap-3">
                      <span className="text-fg-muted">{rowMeta(r)}</span>
                      {canRefund(r) && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          loading={refundingId === r.id}
                          onClick={() => setConfirm({ kind: "refund", row: r })}
                        >
                          {m.regs_refund()}
                        </Button>
                      )}
                      {r.status === "refund_requested" && (
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          loading={denyingId === r.id}
                          onClick={() => setConfirm({ kind: "deny", row: r })}
                        >
                          {m.regs_deny()}
                        </Button>
                      )}
                      {(r.status === "paid" ||
                        r.status === "pending_payment" ||
                        r.status === "waitlisted") && (
                        <Button
                          type="button"
                          size="sm"
                          variant={r.hasPayment ? "danger" : "outline"}
                          loading={removingId === r.id}
                          onClick={() =>
                            setConfirm({ kind: "remove", row: r, keepPayment: r.hasPayment })
                          }
                        >
                          {r.hasPayment ? m.regs_remove_keep() : m.regs_remove()}
                        </Button>
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
      <ConfirmDialog
        open={confirm != null}
        onClose={() => setConfirm(null)}
        title={
          confirm?.kind === "deny"
            ? m.regs_deny()
            : confirm?.kind === "remove"
              ? confirm.keepPayment
                ? m.regs_remove_keep()
                : m.regs_remove()
              : m.regs_refund()
        }
        message={confirmMessage()}
        confirmLabel={
          confirm?.kind === "deny"
            ? m.regs_deny()
            : confirm?.kind === "remove"
              ? confirm.keepPayment
                ? m.regs_remove_keep()
                : m.regs_remove()
              : m.regs_refund()
        }
        pending={refund.isPending || deny.isPending || remove.isPending}
        danger={confirm?.kind === "remove" && confirm.keepPayment}
        onConfirm={() => {
          if (confirm == null) return;
          if (confirm.kind === "remove") {
            remove.mutate(
              { registrationId: confirm.row.id, keepPayment: confirm.keepPayment },
              { onSettled: () => setConfirm(null) },
            );
            return;
          }
          const mut = confirm.kind === "deny" ? deny : refund;
          mut.mutate(confirm.row.id, { onSettled: () => setConfirm(null) });
        }}
      />
    </section>
  );
}
