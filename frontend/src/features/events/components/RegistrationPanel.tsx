import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventDetail } from "../api";
import { useCancelRegistration, usePay, useRegister } from "../api";
import { remainingLabel } from "../lib/countdown";

/**
 * The registration CTA block. Confirming state (checkout=success while
 * the webhook is pending) is rendered by the parent, which also drives
 * the polling — this component only renders server truth.
 */
export function RegistrationPanel({
  event,
  checkoutCancelled,
}: {
  event: EventDetail;
  checkoutCancelled: boolean;
}) {
  const register = useRegister(event.id);
  const pay = usePay(event.id);
  const cancel = useCancelRegistration(event.id);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const reg = event.myRegistration;
  const taken = event.paidCount + event.pendingCount;
  const full = taken >= event.maxParticipants;
  const registrable = event.status === "published";
  const err = register.error ?? pay.error ?? cancel.error;

  const isFree = event.feeCents === 0;
  const deadline = event.refundDeadline ? new Date(event.refundDeadline) : new Date(event.startsAt);
  const pastDeadline = Date.now() > deadline.getTime();

  const confirmCancel = () => {
    setConfirmOpen(false);
    cancel.mutate(undefined);
  };

  if (!registrable && !reg) return null;

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4">
      {err && (
        <p role="alert" className="text-sm text-danger">
          {err.message}
        </p>
      )}
      {!reg && registrable && (
        <Button
          type="button"
          disabled={register.isPending}
          onClick={() => register.mutate(undefined)}
        >
          {full ? m.event_join_waitlist() : m.event_register()}
        </Button>
      )}
      {reg?.status === "pending_payment" && (
        <>
          {checkoutCancelled && (
            <p className="text-sm text-fg-muted">{m.event_checkout_cancelled()}</p>
          )}
          {reg.expiresAt && (
            <p className="text-sm text-fg-muted">
              {m.event_pay_time_left({ remaining: remainingLabel(reg.expiresAt) })}
            </p>
          )}
          <div className="flex gap-2">
            <Button type="button" disabled={pay.isPending} onClick={() => pay.mutate(undefined)}>
              {m.event_pay_now()}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate(undefined)}
            >
              {m.event_cancel_registration()}
            </Button>
          </div>
        </>
      )}
      {reg?.status === "waitlisted" && (
        <>
          <p className="text-sm font-medium text-fg">
            {m.events_my_waitlisted({ pos: reg.waitlistPos ?? 0 })}
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate(undefined)}
          >
            {m.event_leave_waitlist()}
          </Button>
        </>
      )}
      {reg?.status === "paid" && (
        <>
          <p className="text-sm font-medium text-fg">{m.events_my_paid()}</p>
          {registrable && (
            <Button type="button" variant="outline" onClick={() => setConfirmOpen(true)}>
              {m.event_cancel_registration()}
            </Button>
          )}
          <Dialog
            open={confirmOpen}
            onClose={() => setConfirmOpen(false)}
            title={m.event_cancel_registration()}
          >
            <p className="text-sm text-fg">
              {pastDeadline && !isFree
                ? m.event_cancel_confirm_late()
                : m.event_cancel_confirm({ name: event.name })}
            </p>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setConfirmOpen(false)}>
                {m.dialog_close()}
              </Button>
              <Button
                type="button"
                variant="danger"
                disabled={cancel.isPending}
                onClick={confirmCancel}
              >
                {m.event_cancel_registration()}
              </Button>
            </div>
          </Dialog>
        </>
      )}
      {reg?.status === "refund_requested" && (
        <p className="text-sm text-fg-muted">{m.events_my_refund_requested()}</p>
      )}
      {!isFree &&
        (event.refundDeadline ? (
          <p className="text-xs text-fg-muted">
            {m.event_refund_until({ date: deadline.toLocaleString(getLocale()) })}
          </p>
        ) : (
          <p className="text-xs text-fg-muted">{m.event_refund_until_start()}</p>
        ))}
    </div>
  );
}
