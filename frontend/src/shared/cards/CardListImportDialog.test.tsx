import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { CardListImportDialog } from "./CardListImportDialog";
import type { ResolvedItem } from "./CardListImportDialog";
import type { ImportResolveLine } from "./useResolveCardList";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const match = (scryfallId: string, name: string) => ({
  scryfallId,
  oracleId: `o-${scryfallId}`,
  name,
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test Set",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

const matchedLine = (name: string, quantity: number, scryfallId = "id"): ImportResolveLine => ({
  lineNumber: 1,
  raw: `${quantity} ${name}`,
  quantity,
  status: "matched",
  match: match(scryfallId, name),
});

function stubResolve(lines: ImportResolveLine[]) {
  return vi.fn(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/cards/resolve-list")) {
      return new Response(JSON.stringify({ lines }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    throw new Error(`unexpected fetch: ${url}`);
  });
}

function renderDialog({
  onApply,
  resolved,
  ...rest
}: {
  onApply: (items: ResolvedItem[]) => void;
  resolved: ImportResolveLine[];
  applying?: boolean;
  applyError?: Error | null;
  result?: { added: number; updated: number } | null;
}) {
  vi.stubGlobal("fetch", stubResolve(resolved));
  return render(<CardListImportDialog open onClose={() => {}} onApply={onApply} {...rest} />, {
    wrapper,
  });
}

test("paste → review groups matched/ambiguous/unmatched lines", async () => {
  vi.stubGlobal(
    "fetch",
    stubResolve([
      matchedLine("Lightning Bolt", 4, "bolt"),
      {
        lineNumber: 2,
        raw: "Blot",
        quantity: 1,
        status: "ambiguous",
        suggestions: [match("s1", "Lightning Bolt"), match("s2", "Lightning Blast")],
      },
      { lineNumber: 3, raw: "Gibberish", quantity: 1, status: "unmatched" },
    ]),
  );

  render(<CardListImportDialog open onClose={() => {}} onApply={() => {}} />, { wrapper });
  await userEvent.type(screen.getByLabelText("Card list"), "4 Bolt{enter}Blot{enter}Gibberish");
  await userEvent.click(screen.getByRole("button", { name: "Preview import" }));

  expect(await screen.findByText("Matched (1)")).toBeInTheDocument();
  expect(screen.getByText("Needs a choice (1)")).toBeInTheDocument();
  expect(screen.getByText("Not found (1)")).toBeInTheDocument();
});

test("hands resolved items to onApply instead of committing", async () => {
  const onApply = vi.fn();
  renderDialog({ onApply, resolved: [matchedLine("Lightning Bolt", 2, "bolt")] });

  await userEvent.type(screen.getByLabelText("Card list"), "2 Lightning Bolt");
  await userEvent.click(screen.getByRole("button", { name: /preview import/i }));
  await userEvent.click(await screen.findByRole("button", { name: /add to collection/i }));

  expect(onApply).toHaveBeenCalledWith([
    { card: expect.objectContaining({ name: "Lightning Bolt", scryfallId: "bolt" }), quantity: 2 },
  ]);
});

test("initialLines seeds the review phase directly, skipping the paste form", async () => {
  const onApply = vi.fn();
  render(
    <CardListImportDialog
      open
      onClose={() => {}}
      onApply={onApply}
      initialLines={[matchedLine("Lightning Bolt", 3, "bolt")]}
    />,
    { wrapper },
  );

  expect(screen.queryByLabelText("Card list")).not.toBeInTheDocument();
  expect(screen.getByText("Matched (1)")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: /add to collection/i }));
  expect(onApply).toHaveBeenCalledWith([
    { card: expect.objectContaining({ name: "Lightning Bolt", scryfallId: "bolt" }), quantity: 3 },
  ]);
});

test("back closes a seeded dialog instead of returning to a paste form", async () => {
  const onClose = vi.fn();
  render(
    <CardListImportDialog
      open
      onClose={onClose}
      onApply={() => {}}
      initialLines={[matchedLine("Lightning Bolt", 3, "bolt")]}
    />,
    { wrapper },
  );

  await userEvent.click(screen.getByRole("button", { name: "Back" }));
  expect(onClose).toHaveBeenCalled();
});

test("renders the passed-in result instead of committing itself", () => {
  render(
    <CardListImportDialog
      open
      onClose={() => {}}
      onApply={() => {}}
      result={{ added: 2, updated: 1 }}
    />,
    { wrapper },
  );
  expect(screen.getByText("2 new cards, 1 updated.")).toBeInTheDocument();
});
