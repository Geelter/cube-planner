import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { CardPreviewSheet } from "./CardPreviewSheet";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const printings = [
  {
    scryfallId: "s1",
    oracleId: "o1",
    name: "Valki, God of Lies",
    manaCost: "{1}{B}",
    typeLine: "Legendary Creature — God",
    oracleText: "Valki does things.",
    setName: "Kaldheim",
    setCode: "khm",
    collectorNumber: "114",
    rarity: "mythic",
    releasedAt: "2021-02-05",
    cmc: 2,
    colors: ["B", "R"],
    colorIdentity: ["B", "R"],
    promo: false,
    imageSmall: "https://img/s1-small.jpg",
    imageNormal: "https://img/s1.jpg",
    backImageNormal: "https://img/s1-back.jpg",
  },
  {
    scryfallId: "s2",
    oracleId: "o1",
    name: "Valki, God of Lies",
    manaCost: "{1}{B}",
    typeLine: "Legendary Creature — God",
    oracleText: "Valki does things.",
    setName: "Kaldheim Promos",
    setCode: "pkhm",
    collectorNumber: "114p",
    rarity: "mythic",
    releasedAt: "2021-02-05",
    cmc: 2,
    colors: ["B", "R"],
    colorIdentity: ["B", "R"],
    promo: true,
    imageSmall: null,
    imageNormal: "https://img/s2.jpg",
    backImageNormal: null,
  },
];

function stubPrintingsFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ printings }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

test("shows the row's printing with details, both faces, and printing count", async () => {
  stubPrintingsFetch();
  render(
    <CardPreviewSheet
      card={{ oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" }}
      onClose={() => {}}
    />,
    { wrapper },
  );
  expect(await screen.findByText("Valki does things.")).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "Valki, God of Lies" })).toHaveAttribute(
    "src",
    "https://img/s1.jpg",
  );
  expect(screen.getByRole("img", { name: "Back face of Valki, God of Lies" })).toHaveAttribute(
    "src",
    "https://img/s1-back.jpg",
  );
  expect(screen.getByText("Kaldheim · #114")).toBeInTheDocument();
});

test("without a scryfallId falls back to the first printing", async () => {
  stubPrintingsFetch();
  render(
    <CardPreviewSheet card={{ oracleId: "o1", name: "Valki, God of Lies" }} onClose={() => {}} />,
    { wrapper },
  );
  expect(await screen.findByText("Kaldheim · #114")).toBeInTheDocument();
});

test("change-printing button renders only when a handler is provided", async () => {
  stubPrintingsFetch();
  const onChangePrinting = vi.fn();
  const card = { oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" };
  render(<CardPreviewSheet card={card} onClose={() => {}} onChangePrinting={onChangePrinting} />, {
    wrapper,
  });
  await userEvent.click(await screen.findByRole("button", { name: "Change printing" }));
  expect(onChangePrinting).toHaveBeenCalledWith(card);
});

test("no change-printing button without a handler; close calls onClose", async () => {
  stubPrintingsFetch();
  const onClose = vi.fn();
  render(
    <CardPreviewSheet
      card={{ oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" }}
      onClose={onClose}
    />,
    { wrapper },
  );
  await screen.findByText("Valki does things.");
  expect(screen.queryByRole("button", { name: "Change printing" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});
