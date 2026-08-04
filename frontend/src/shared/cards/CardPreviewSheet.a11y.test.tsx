// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { axe } from "vitest-axe";
import { CardPreviewSheet } from "./CardPreviewSheet";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function stubEnv(desktop: boolean) {
  // jsdom has no matchMedia at all — stub it for each shell.
  vi.stubGlobal("matchMedia", () => ({
    matches: desktop,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          printings: [
            {
              scryfallId: "s1",
              oracleId: "o1",
              name: "Sol Ring",
              manaCost: "{1}",
              typeLine: "Artifact",
              oracleText: "Add {C}{C}.",
              setName: "Test",
              setCode: "tst",
              collectorNumber: "1",
              rarity: "uncommon",
              releasedAt: "2020-01-01",
              cmc: 1,
              colors: [],
              colorIdentity: [],
              promo: false,
              imageSmall: null,
              imageNormal: "https://img/s1.jpg",
              backImageNormal: null,
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
}

for (const [shell, desktop] of [
  ["dialog", true],
  ["drawer", false],
] as const) {
  test(`${shell} shell has no axe violations`, async () => {
    stubEnv(desktop);
    const { container, findByText } = render(
      <CardPreviewSheet
        card={{ oracleId: "o1", scryfallId: "s1", name: "Sol Ring" }}
        onClose={() => {}}
      />,
      { wrapper },
    );
    await findByText("Add {C}{C}.");
    expect(await axe(container)).toHaveNoViolations();
  });
}
