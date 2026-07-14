import { useState } from "react";
import type { FormEvent } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import type { EventDetail, EventFormValues } from "../api";

// datetime-local wants "YYYY-MM-DDTHH:mm" in local time.
function toLocalInput(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string | undefined {
  return v ? new Date(v).toISOString() : undefined;
}

export function EventForm({
  initial,
  locked,
  submitLabel,
  onSubmit,
  pending,
  error,
}: {
  initial?: EventDetail;
  /** true once published: name/date/fee/participants are frozen. */
  locked: boolean;
  submitLabel: string;
  onSubmit: (values: EventFormValues) => void;
  pending: boolean;
  error: Error | null;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [location, setLocation] = useState(initial?.location ?? "");
  const [startsAt, setStartsAt] = useState(toLocalInput(initial?.startsAt));
  const [feePln, setFeePln] = useState(initial ? String(initial.feeCents / 100) : "0");
  const [maxParticipants, setMaxParticipants] = useState(String(initial?.maxParticipants ?? 8));
  const [refundDeadline, setRefundDeadline] = useState(
    toLocalInput(initial?.refundDeadline ?? undefined),
  );

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const refund = fromLocalInput(refundDeadline);
    onSubmit({
      name,
      description,
      location,
      startsAt: fromLocalInput(startsAt) ?? new Date().toISOString(),
      feeCents: Math.round(Number(feePln) * 100),
      maxParticipants: Number(maxParticipants),
      ...(refund ? { refundDeadline: refund } : {}),
    });
  };

  const lockedHint = locked ? (
    <span className="text-xs text-fg-muted"> ({m.event_form_locked_hint()})</span>
  ) : null;

  return (
    <form onSubmit={submit} className="flex max-w-lg flex-col gap-4">
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error.message}
        </p>
      )}
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-name">
          {m.event_form_name()}
          {lockedHint}
        </Label>
        <Input
          id="ev-name"
          required
          maxLength={200}
          disabled={locked}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-desc">{m.event_form_description()}</Label>
        <textarea
          id="ev-desc"
          className="rounded-md border border-border bg-surface px-3 py-2 text-fg"
          rows={4}
          maxLength={5000}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-location">{m.event_form_location()}</Label>
        <Input
          id="ev-location"
          maxLength={200}
          value={location}
          onChange={(e) => setLocation(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-starts">
          {m.event_form_starts_at()}
          {lockedHint}
        </Label>
        <Input
          id="ev-starts"
          type="datetime-local"
          required
          disabled={locked}
          value={startsAt}
          onChange={(e) => setStartsAt(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-fee">
          {m.event_form_fee()}
          {lockedHint}
        </Label>
        <Input
          id="ev-fee"
          type="number"
          min="0"
          step="0.01"
          required
          disabled={locked}
          value={feePln}
          onChange={(e) => setFeePln(e.target.value)}
          aria-describedby="ev-fee-hint"
        />
        <p id="ev-fee-hint" className="text-xs text-fg-muted">
          {m.event_form_fee_hint()}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-max">
          {m.event_form_max_participants()}
          {lockedHint}
        </Label>
        <Input
          id="ev-max"
          type="number"
          min="1"
          max="1000"
          required
          disabled={locked}
          value={maxParticipants}
          onChange={(e) => setMaxParticipants(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-refund">{m.event_form_refund_deadline()}</Label>
        <Input
          id="ev-refund"
          type="datetime-local"
          value={refundDeadline}
          onChange={(e) => setRefundDeadline(e.target.value)}
        />
      </div>
      <Button type="submit" disabled={pending}>
        {submitLabel}
      </Button>
    </form>
  );
}
