import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import type { EventRegistrationRow, EventSummary } from "../api";
import { useDenyRefund, useEventRegistrations, useRefundRegistration } from "../api";

// `queue` is the only group that outlives `published`: a player who
// self-cancels past the refund deadline lands in refund_requested, and the
// organizer still needs a screen to resolve it after the event starts.
const GROUPS: { key: string; title: () => string; statuses: string[]; publishedOnly: boolean }[] = [
  { key: "paid", title: () => m.regs_group_paid(), statuses: ["paid"], publishedOnly: true },
  {
    key: "pending",
    title: () => m.regs_group_pending(),
    statuses: ["pending_payment"],
    publishedOnly: true,
  },
  {
    key: "waitlist",
    title: () => m.regs_group_waitlist(),
    statuses: ["waitlisted"],
    publishedOnly: true,
  },
  {
    key: "queue",
    title: () => m.regs_group_refund_queue(),
    statuses: ["refund_requested"],
    publishedOnly: false,
  },
  {
    key: "history",
    title: () => m.regs_group_history(),
    statuses: ["cancelled", "refunded", "expired", "removed"],
    publishedOnly: true,
  },
];

type Confirm = { kind: "refund" | "deny"; row: EventRegistrationRow };

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
  const visible = GROUPS.filter((g) =>
    published ? true : !g.publishedOnly && rowsFor(g.statuses).length > 0,
  );
  if (visible.length === 0) return null;

  const err = refund.error ?? deny.error;
  const locale = getLocale();
  const refundingId = refund.isPending ? refund.variables : null;
  const denyingId = deny.isPending ? deny.variables : null;

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
                      {(r.status === "refund_requested" || r.status === "paid") && (
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
        title={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
        message={
          confirm == null
            ? ""
            : confirm.kind === "deny"
              ? m.regs_deny_confirm({ name: confirm.row.displayName })
              : m.regs_refund_confirm({ name: confirm.row.displayName })
        }
        confirmLabel={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
        pending={refund.isPending || deny.isPending}
        onConfirm={() => {
          if (confirm == null) return;
          const mut = confirm.kind === "deny" ? deny : refund;
          mut.mutate(confirm.row.id, { onSettled: () => setConfirm(null) });
        }}
      />
    </section>
  );
}
