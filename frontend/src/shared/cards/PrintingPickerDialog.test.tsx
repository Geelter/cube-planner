import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { PrintingPickerDialog } from "./PrintingPickerDialog";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

const printings = [
  { scryfallId: "new", setName: "Magic 2010", collectorNumber: "146", imageSmall: null },
  { scryfallId: "old", setName: "Limited Edition Alpha", collectorNumber: "161", imageSmall: null },
];

test("lists printings, marks the current one, picks another", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ printings }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  const onPick = vi.fn();
  render(
    <PrintingPickerDialog
      open
      onClose={() => {}}
      oracleId="o1"
      name="Lightning Bolt"
      currentScryfallId="new"
      onPick={onPick}
    />,
    { wrapper },
  );
  expect(await screen.findByText(/Magic 2010/)).toBeInTheDocument();
  // Current printing is marked and not pickable.
  expect(screen.getByText("Current")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /Limited Edition Alpha/ }));
  expect(onPick).toHaveBeenCalledWith("old");
});

test("current printing is keyboard-reachable and announced via aria-current", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ printings }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  render(
    <PrintingPickerDialog
      open
      onClose={() => {}}
      oracleId="o1"
      name="Lightning Bolt"
      currentScryfallId="new"
      onPick={() => {}}
    />,
    { wrapper },
  );
  // The current printing must be a real, focusable element (not a
  // visual-only span) with aria-current so keyboard/AT users can tell
  // which printing they already own.
  const current = await screen.findByRole("button", { name: /Magic 2010/ });
  expect(current).toHaveAttribute("aria-current", "true");
});
