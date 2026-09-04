import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { CardListImportDialog } from "@/shared/cards/CardListImportDialog";
import type { ResolvedItem } from "@/shared/cards/CardListImportDialog";
import { useResolveCardList } from "@/shared/cards/useResolveCardList";
import type { ImportResolveLine } from "@/shared/cards/useResolveCardList";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useCreateCube } from "../api";
import "../lib/importHandoff";

export function CreateCubePage() {
  const navigate = useNavigate();
  const create = useCreateCube();
  const resolve = useResolveCardList();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"public" | "private">("public");
  const [cardText, setCardText] = useState("");
  const [review, setReview] = useState<ImportResolveLine[] | null>(null);

  // Resolve already happened (side-effect-free); this only runs once the
  // reviewer applies a non-empty selection, so create the cube now and hand
  // the staged items to the editor as change #1.
  const createAndStage = (items: ResolvedItem[]) => {
    create.mutate(
      { name, description, visibility },
      {
        onSuccess: (cube) =>
          void navigate({
            to: "/cubes/$cubeId/edit",
            params: { cubeId: cube.id },
            state: { importedItems: items },
          }),
      },
    );
  };

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
              if (cardText.trim() === "") {
                create.mutate(
                  { name, description, visibility },
                  {
                    onSuccess: (cube) =>
                      void navigate({ to: "/cubes/$cubeId", params: { cubeId: cube.id } }),
                  },
                );
                return;
              }
              // Resolve first: a list that resolves to nothing must not
              // leave an orphan empty cube behind.
              resolve.mutate({ text: cardText }, { onSuccess: (lines) => setReview(lines) });
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
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="cube-cards">{m.cubes_create_cards_label()}</Label>
              <textarea
                id="cube-cards"
                rows={8}
                className="rounded-md border border-border bg-surface p-2 font-mono text-sm text-fg"
                value={cardText}
                onChange={(e) => setCardText(e.target.value)}
              />
              <p className="text-sm text-fg-muted">{m.cubes_create_cards_hint()}</p>
            </div>
            {create.isError && <Alert variant="danger">{create.error.message}</Alert>}
            {resolve.isError && <Alert variant="danger">{resolve.error.message}</Alert>}
            <Button type="submit" loading={create.isPending || resolve.isPending}>
              {m.cubes_create_submit()}
            </Button>
          </form>
        </CardContent>
      </Card>

      {review !== null && (
        <CardListImportDialog
          open
          onClose={() => setReview(null)}
          initialLines={review}
          applying={create.isPending}
          applyError={create.error}
          onApply={createAndStage}
        />
      )}
    </div>
  );
}
