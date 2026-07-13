import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { ImportDialog } from "./ImportDialog";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

const card = (scryfallId: string, name: string) => ({
  scryfallId,
  oracleId: "o",
  name,
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test Set",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

test("paste → review groups → confirm posts matched + default ambiguous choice", async () => {
  const fetchMock = vi.fn(async (input: Request | string, _init?: RequestInit) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/import/resolve")) {
      return new Response(
        JSON.stringify({
          lines: [
            {
              lineNumber: 1,
              raw: "4 Bolt",
              quantity: 4,
              status: "matched",
              match: card("bolt", "Lightning Bolt"),
            },
            {
              lineNumber: 2,
              raw: "Blot",
              quantity: 1,
              status: "ambiguous",
              suggestions: [card("s1", "Lightning Bolt"), card("s2", "Lightning Blast")],
            },
            { lineNumber: 3, raw: "Gibberish", quantity: 1, status: "unmatched" },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }
    return new Response(JSON.stringify({ addedRows: 2, updatedRows: 0 }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<ImportDialog open onClose={() => {}} />, { wrapper });
  await userEvent.type(screen.getByLabelText("Card list"), "4 Bolt{enter}Blot{enter}Gibberish");
  await userEvent.click(screen.getByRole("button", { name: "Preview import" }));

  expect(await screen.findByText("Matched (1)")).toBeInTheDocument();
  expect(screen.getByText("Needs a choice (1)")).toBeInTheDocument();
  expect(screen.getByText("Not found (1)")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Add to collection (2)" }));
  expect(await screen.findByText("2 new cards, 0 updated.")).toBeInTheDocument();

  const importCall = fetchMock.mock.calls.find(([input]) => {
    const url = typeof input === "string" ? input : input.url;
    return url.endsWith("/api/collection/import");
  });
  expect(importCall).toBeDefined();
  const [input, init] = importCall as [Request | string, RequestInit | undefined];
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const rawBody =
    init?.body ?? (typeof input === "string" ? undefined : await input.clone().text());
  const body = JSON.parse(rawBody as string);
  expect(body.items).toEqual([
    { scryfallId: "bolt", quantity: 4 },
    { scryfallId: "s1", quantity: 1 },
  ]);
});
