import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventRegistrationRow } from "../api";
import { useDenyRefund, useEventRegistrations, useRefundRegistration } from "../api";

const GROUPS: { key: string; title: () => string; statuses: string[] }[] = [
  { key: "paid", title: () => m.regs_group_paid(), statuses: ["paid"] },
  { key: "pending", title: () => m.regs_group_pending(), statuses: ["pending_payment"] },
  { key: "waitlist", title: () => m.regs_group_waitlist(), statuses: ["waitlisted"] },
  { key: "queue", title: () => m.regs_group_refund_queue(), statuses: ["refund_requested"] },
  {
    key: "history",
    title: () => m.regs_group_history(),
    statuses: ["cancelled", "refunded", "expired"],
  },
];

type Confirm = { kind: "refund" | "deny"; row: EventRegistrationRow };

export function RegistrationsTable({ eventId }: { eventId: string }) {
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

  const err = refund.error ?? deny.error;
  const locale = getLocale();

  const rowMeta = (r: EventRegistrationRow) => {
    if (r.status === "pending_payment" && r.expiresAt) {
      return m.regs_expires({ date: new Date(r.expiresAt).toLocaleString(locale) });
    }
    if (r.status === "paid" && r.paidAt) {
      return m.regs_paid_at({ date: new Date(r.paidAt).toLocaleString(locale) });
    }
    if (r.status === "waitlisted" && r.waitlistPos != null) return `#${r.waitlistPos}`;
    return r.status;
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.regs_title()}</h2>
      {err && (
        <p role="alert" className="text-sm text-danger">
          {err.message}
        </p>
      )}
      {GROUPS.map((g) => {
        const rows = (regs.data ?? [])
          .filter((r) => g.statuses.includes(r.status))
          .sort((a, b) => (a.waitlistPos ?? 0) - (b.waitlistPos ?? 0));
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
                          disabled={refund.isPending}
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
                          disabled={deny.isPending}
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
      <Dialog
        open={confirm != null}
        onClose={() => setConfirm(null)}
        title={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
      >
        <p className="text-sm text-fg">
          {confirm?.kind === "deny"
            ? m.regs_deny_confirm({ name: confirm.row.displayName })
            : confirm
              ? m.regs_refund_confirm({ name: confirm.row.displayName })
              : ""}
        </p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirm(null)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            onClick={() => {
              if (confirm?.kind === "refund") refund.mutate(confirm.row.id);
              if (confirm?.kind === "deny") deny.mutate(confirm.row.id);
              setConfirm(null);
            }}
          >
            {confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
          </Button>
        </div>
      </Dialog>
    </section>
  );
}
