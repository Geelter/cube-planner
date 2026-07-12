import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useCreateCube } from "../api";

export function CreateCubePage() {
  const navigate = useNavigate();
  const create = useCreateCube();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private">("public");

  return (
    <div className="mx-auto w-full max-w-md">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.cubes_create_title()}</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(
                { name, description, visibility },
                {
                  onSuccess: (cube) =>
                    void navigate({ to: "/cubes/$cubeId", params: { cubeId: cube.id } }),
                },
              );
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="cube-name">{m.cubes_field_name()}</Label>
              <Input
                id="cube-name"
                required
                maxLength={100}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="cube-description">{m.cubes_field_description()}</Label>
              <textarea
                id="cube-description"
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
                    name="visibility"
                    value="public"
                    checked={visibility === "public"}
                    onChange={() => setVisibility("public")}
                  />
                  {m.cubes_visibility_public()}
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input
                    type="radio"
                    name="visibility"
                    value="private"
                    checked={visibility === "private"}
                    onChange={() => setVisibility("private")}
                  />
                  {m.cubes_visibility_private()}
                </label>
              </div>
            </fieldset>
            {create.isError && <Alert variant="danger">{create.error.message}</Alert>}
            <Button type="submit" disabled={create.isPending}>
              {m.cubes_create_submit()}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
