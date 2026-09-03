import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { m } from "@/paraglide/messages";

const refundMutate = vi.fn();
const denyMutate = vi.fn();
const refundState: { isPending: boolean; variables?: string } = { isPending: false };
const denyState: { isPending: boolean; variables?: string } = { isPending: false };
const defaultRows = [
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
// Reassigned per-test so the status-gating tests can control which rows the
// mocked query returns without a second `vi.mock` module.
let regsData: typeof defaultRows = defaultRows;

vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventRegistrations: () => ({ data: regsData, isPending: false, error: null }),
  useRefundRegistration: () => ({ mutate: refundMutate, error: null, ...refundState }),
  useDenyRefund: () => ({ mutate: denyMutate, error: null, ...denyState }),
}));

import type { EventSummary } from "../api";
import { RegistrationsTable } from "./RegistrationsTable";

afterEach(() => {
  cleanup();
  refundState.isPending = false;
  delete refundState.variables;
  denyState.isPending = false;
  delete denyState.variables;
  regsData = defaultRows;
});

function renderTable(status: EventSummary["status"] = "published") {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" status={status} />
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
  // The expired history row renders the localized label, not the raw
  // backend enum string.
  expect(screen.getByText("Expired")).toBeInTheDocument();
  expect(screen.queryByText("expired")).not.toBeInTheDocument();
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

test("only the acted-on row's refund button spins; other rows stay enabled", () => {
  refundState.isPending = true;
  refundState.variables = "r1"; // the paid row
  renderTable();
  // r1 (paid) and r3 (refund_requested) both render a refund button.
  const refundButtons = screen
    .getAllByRole("button")
    .filter((b) => b.textContent === m.regs_refund());
  const busy = refundButtons.filter((b) => b.getAttribute("aria-busy") === "true");
  expect(busy).toHaveLength(1);
  const idle = refundButtons.find((b) => b.getAttribute("aria-busy") !== "true");
  expect(idle).toBeEnabled();
});

test("published shows every group", () => {
  renderTable("published");
  expect(screen.getByRole("heading", { name: m.regs_title() })).toBeInTheDocument();
  expect(screen.getByText("Ala")).toBeInTheDocument();
});

test("draft renders nothing when the refund queue is empty", () => {
  regsData = [defaultRows[0]!]; // paid only, no refund_requested row
  renderTable("draft");
  expect(screen.queryByRole("heading", { name: m.regs_title() })).not.toBeInTheDocument();
  expect(screen.queryByText("Ala")).not.toBeInTheDocument();
});

test("started shows only the refund queue", () => {
  regsData = [defaultRows[0]!, defaultRows[2]!]; // paid (Ala) + refund_requested (Cez)
  renderTable("started");
  expect(screen.getByText("Cez")).toBeInTheDocument();
  expect(screen.queryByText("Ala")).not.toBeInTheDocument();
});

test("finished with an empty queue renders nothing", () => {
  regsData = [defaultRows[0]!]; // paid only
  renderTable("finished");
  expect(screen.queryByRole("heading", { name: m.regs_title() })).not.toBeInTheDocument();
  expect(screen.queryByText("Ala")).not.toBeInTheDocument();
});
