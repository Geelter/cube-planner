import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CardSearchPage } from "./CardSearchPage";

const bolt = {
  scryfallId: "ce711943-c1a1-43a0-8b89-8d169cfb8e06",
  oracleId: "4457ed35-7c10-48c8-9776-456485fdf070",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  imageSmall: "https://img.test/bolt-s.jpg",
};

const boltPrinting = {
  ...bolt,
  oracleText: "Lightning Bolt deals 3 damage to any target.",
  setCode: "m11",
  setName: "Magic 2011",
  collectorNumber: "149",
  rarity: "common",
  releasedAt: "2010-07-16",
  cmc: 1,
  colors: ["R"],
  colorIdentity: ["R"],
  promo: false,
  imageNormal: "https://img.test/bolt-n.jpg",
  backImageNormal: null,
};

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test("search, select, see details", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      if (url.pathname === "/api/cards/autocomplete") {
        return jsonResponse({ cards: [bolt] });
      }
      if (url.pathname === `/api/cards/${bolt.oracleId}/printings`) {
        return jsonResponse({ printings: [boltPrinting] });
      }
      return new Response("{}", { status: 404 });
    }),
  );

  const user = userEvent.setup();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <CardSearchPage />
    </QueryClientProvider>,
  );

  await user.type(screen.getByRole("combobox"), "bolt");
  // Debounced fetch → option appears.
  const option = await screen.findByRole("option", { name: /Lightning Bolt/ });
  await user.click(option);

  // Details panel from the printings endpoint.
  expect(
    await screen.findByRole("heading", { level: 2, name: "Lightning Bolt" }),
  ).toBeInTheDocument();
  expect(screen.getByText(/deals 3 damage/)).toBeInTheDocument();
  expect(screen.getByText(/Magic 2011/)).toBeInTheDocument();
});
