import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";

const refundMutate = vi.fn();
const denyMutate = vi.fn();
const rows = [
  {
    id: "r1",
    status: "paid",
    displayName: "Ala",
    email: "ala@t",
    createdAt: "2026-07-13T10:00:00Z",
    paidAt: "2026-07-13T10:05:00Z",
  },
  {
    id: "r2",
    status: "waitlisted",
    displayName: "Bea",
    email: "bea@t",
    createdAt: "2026-07-13T10:01:00Z",
    waitlistPos: 1,
  },
  {
    id: "r3",
    status: "refund_requested",
    displayName: "Cez",
    email: "cez@t",
    createdAt: "2026-07-13T10:02:00Z",
  },
  {
    id: "r4",
    status: "expired",
    displayName: "Dag",
    email: "dag@t",
    createdAt: "2026-07-13T10:03:00Z",
  },
];
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventRegistrations: () => ({ data: rows, isPending: false, error: null }),
  useRefundRegistration: () => ({ mutate: refundMutate, isPending: false, error: null }),
  useDenyRefund: () => ({ mutate: denyMutate, isPending: false, error: null }),
}));

import { RegistrationsTable } from "./RegistrationsTable";

afterEach(cleanup);

function renderTable() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" />
    </QueryClientProvider>,
  );
}

test("groups rows and gates actions by status", () => {
  renderTable();
  expect(screen.getByText("Paid roster")).toBeInTheDocument();
  expect(screen.getByText("Refund queue")).toBeInTheDocument();
  expect(screen.getByText("#1")).toBeInTheDocument();
  // paid + refund_requested rows each get a Refund button; only the
  // queued row gets Deny.
  expect(screen.getAllByRole("button", { name: "Refund" })).toHaveLength(2);
  expect(screen.getAllByRole("button", { name: "Deny" })).toHaveLength(1);
});

test("refund flows through the confirm dialog", async () => {
  renderTable();
  await userEvent.click(screen.getAllByRole("button", { name: "Refund" })[1]!);
  expect(await screen.findByText(/Refund Cez's entry fee\?/)).toBeInTheDocument();
  // The dialog's action button is the last "Refund" in the DOM.
  const buttons = screen.getAllByRole("button", { name: "Refund" });
  await userEvent.click(buttons[buttons.length - 1]!);
  expect(refundMutate).toHaveBeenCalledWith("r3");
});
