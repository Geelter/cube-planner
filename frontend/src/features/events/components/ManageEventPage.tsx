import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventAction } from "../api";
import { UnauthorizedError, useEvent, useEventAction, useUpdateEvent } from "../api";
import { EventStatusBadge } from "./EventsListPage";
import { EventCubesEditor } from "./EventCubesEditor";
import { EventForm } from "./EventForm";
import { RegistrationsTable } from "./RegistrationsTable";

const ACTIONS: {
  action: EventAction;
  label: () => string;
  confirm: (name: string) => string;
  from: string[];
}[] = [
  {
    action: "publish",
    label: () => m.event_lifecycle_publish(),
    confirm: (name) => m.event_publish_confirm({ name }),
    from: ["draft"],
  },
  {
    action: "start",
    label: () => m.event_lifecycle_start(),
    confirm: (name) => m.event_start_confirm({ name }),
    from: ["published"],
  },
  {
    action: "finish",
    label: () => m.event_lifecycle_finish(),
    confirm: (name) => m.event_finish_confirm({ name }),
    from: ["started"],
  },
  {
    action: "cancel",
    label: () => m.event_lifecycle_cancel(),
    confirm: (name) => m.event_cancel_event_confirm({ name }),
    from: ["published", "started"],
  },
];

export function ManageEventPage() {
  const { eventId } = useParams({ from: "/events/$eventId/manage" });
  const me = useMe();
  const event = useEvent(eventId);
  const update = useUpdateEvent(eventId);
  const act = useEventAction(eventId);
  const [confirmAction, setConfirmAction] = useState<EventAction | null>(null);
  const [saved, setSaved] = useState(false);

  if (me.isPending || event.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (me.data?.role !== "admin") return <p className="text-fg-muted">{m.events_not_found()}</p>;
  if (event.error instanceof UnauthorizedError)
    return <p className="text-fg-muted">{event.error.message}</p>;
  if (event.error)
    return (
      <p role="alert" className="text-danger">
        {m.events_not_found()}
      </p>
    );

  const e = event.data;
  const pendingConfirm = ACTIONS.find((a) => a.action === confirmAction);

  return (
    <div className="flex flex-col gap-8">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="flex items-center gap-2 text-2xl font-semibold text-fg">
          <Link to="/events/$eventId" params={{ eventId }} className="hover:text-accent">
            {e.name}
          </Link>
          <EventStatusBadge status={e.status} />
        </h1>
        <div className="flex flex-wrap gap-2">
          {ACTIONS.filter((a) => a.from.includes(e.status)).map((a) => (
            <Button
              key={a.action}
              type="button"
              size="sm"
              {...(a.action === "cancel" ? { variant: "outline" as const } : {})}
              onClick={() => setConfirmAction(a.action)}
            >
              {a.label()}
            </Button>
          ))}
        </div>
      </div>

      {act.error && (
        <p role="alert" className="text-sm text-danger">
          {act.error.message}
        </p>
      )}

      <EventForm
        initial={e}
        locked={e.status !== "draft"}
        submitLabel={m.event_form_save()}
        pending={update.isPending}
        error={update.error}
        onSubmit={(values) => {
          const body =
            e.status === "draft"
              ? values
              : {
                  description: values.description,
                  location: values.location,
                  ...(values.refundDeadline ? { refundDeadline: values.refundDeadline } : {}),
                };
          update.mutate(body, { onSuccess: () => setSaved(true) });
        }}
      />
      {saved && <output className="text-sm text-fg-muted">{m.event_form_saved()}</output>}

      <EventCubesEditor event={e} />

      <RegistrationsTable eventId={eventId} />

      <Dialog
        open={confirmAction != null}
        onClose={() => setConfirmAction(null)}
        title={pendingConfirm?.label() ?? ""}
      >
        <p className="text-sm text-fg">{pendingConfirm?.confirm(e.name)}</p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirmAction(null)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            disabled={act.isPending}
            onClick={() => {
              if (confirmAction) act.mutate(confirmAction);
              setConfirmAction(null);
            }}
          >
            {pendingConfirm?.label()}
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
