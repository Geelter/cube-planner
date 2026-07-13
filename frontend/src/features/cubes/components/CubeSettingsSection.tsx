import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import type { CubeDetail } from "../api";
import { useDeleteCube, useUpdateCube } from "../api";

export function CubeSettingsSection({ cube }: { cube: CubeDetail }) {
  const navigate = useNavigate();
  const update = useUpdateCube(cube.id);
  const del = useDeleteCube(cube.id);
  const [name, setName] = useState(cube.name);
  const [description, setDescription] = useState(cube.description);
  const [visibility, setVisibility] = useState<"public" | "private">(
    cube.visibility === "private" ? "private" : "public",
  );

  return (
    <section className="flex max-w-md flex-col gap-4 rounded-lg border border-border p-4">
      <h2 className="text-lg font-semibold text-fg">{m.cubes_edit_meta()}</h2>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate({ name, description, visibility });
        }}
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="settings-name">{m.cubes_field_name()}</Label>
          <Input
            id="settings-name"
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="settings-description">{m.cubes_field_description()}</Label>
          <textarea
            id="settings-description"
            className="min-h-24 rounded-md border border-border bg-surface px-3 py-2 text-sm text-fg"
            maxLength={2000}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <fieldset className="flex flex-col gap-1.5">
          <legend className="text-sm font-medium text-fg">{m.cubes_field_visibility()}</legend>
          <div className="flex gap-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="settings-visibility"
                value="public"
                checked={visibility === "public"}
                onChange={() => setVisibility("public")}
              />
              {m.cubes_visibility_public()}
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="settings-visibility"
                value="private"
                checked={visibility === "private"}
                onChange={() => setVisibility("private")}
              />
              {m.cubes_visibility_private()}
            </label>
          </div>
        </fieldset>
        {update.isError && <Alert variant="danger">{update.error.message}</Alert>}
        <Button type="submit" disabled={update.isPending}>
          {m.cubes_save_meta()}
        </Button>
      </form>
      <hr className="border-border" />
      {del.isError && <Alert variant="danger">{del.error.message}</Alert>}
      <Button
        type="button"
        variant="danger"
        disabled={del.isPending}
        onClick={() => {
          if (window.confirm(m.cubes_delete_confirm())) {
            del.mutate(undefined, { onSuccess: () => void navigate({ to: "/cubes" }) });
          }
        }}
      >
        {m.cubes_delete()}
      </Button>
    </section>
  );
}
