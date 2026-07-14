import { useNavigate } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { useCreateEvent } from "../api";
import { EventForm } from "./EventForm";

export function NewEventPage() {
  const me = useMe();
  const navigate = useNavigate();
  const create = useCreateEvent();

  if (me.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (me.data?.role !== "admin") return <p className="text-fg-muted">{m.events_not_found()}</p>;

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-fg">{m.events_new_title()}</h1>
      <EventForm
        locked={false}
        submitLabel={m.event_form_create()}
        pending={create.isPending}
        error={create.error}
        onSubmit={(values) =>
          create.mutate(values, {
            onSuccess: (ev) =>
              navigate({ to: "/events/$eventId/manage", params: { eventId: ev.id } }),
          })
        }
      />
    </div>
  );
}
