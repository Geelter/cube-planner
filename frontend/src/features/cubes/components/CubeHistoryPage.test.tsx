import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  getRouteApi: () => ({ useParams: () => ({ cubeId: "cube-1" }) }),
  Link: ({ children }: { children: React.ReactNode }) => <a href="/test">{children}</a>,
}));

vi.mock("../api", async (importOriginal) => {
  const original = await importOriginal<typeof import("../api")>();
  return {
    ...original,
    useCube: () => ({
      isPending: false,
      isError: false,
      data: { id: "cube-1", name: "Test Cube", version: 2 },
    }),
    useCubeChanges: () => ({
      isPending: false,
      isError: false,
      data: {
        total: 2,
        changes: [
          {
            id: "ch2",
            version: 2,
            authorName: "Mat",
            note: "trim",
            createdAt: "2026-07-12T12:00:00Z",
            adds: [],
            removes: [{ oracleId: "o-bolt", name: "Lightning Bolt", quantity: 1 }],
          },
          {
            id: "ch1",
            version: 1,
            authorName: "Mat",
            note: "",
            createdAt: "2026-07-12T11:00:00Z",
            adds: [
              { oracleId: "o-bolt", name: "Lightning Bolt", quantity: 2 },
              { oracleId: "o-ring", name: "Sol Ring", quantity: 1 },
            ],
            removes: [],
          },
        ],
      },
    }),
  };
});

import { CubeHistoryPage } from "./CubeHistoryPage";

test("renders changes newest first with add/remove chips", () => {
  render(<CubeHistoryPage />);
  const headings = screen.getAllByRole("heading", { level: 2 });
  expect(headings[0]?.textContent).toContain("2");
  expect(headings[0]?.textContent).toContain("trim");
  expect(screen.getByText(/−.*Lightning Bolt/)).toBeDefined();
  expect(screen.getByText(/\+2 Lightning Bolt/)).toBeDefined();
  expect(screen.getByText(/\+Sol Ring/)).toBeDefined();
});
