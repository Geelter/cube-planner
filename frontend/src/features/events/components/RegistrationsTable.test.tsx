import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { m } from "@/paraglide/messages";
import type { EventRegistrationRow } from "../api";

const refundMutate = vi.fn();
const denyMutate = vi.fn();
const removeMutate = vi.fn();
const refundState: { isPending: boolean; variables?: string } = { isPending: false };
const denyState: { isPending: boolean; variables?: string } = { isPending: false };
const removeState: {
  isPending: boolean;
  variables?: { registrationId: string; keepPayment: boolean };
} = { isPending: false };
const defaultRows: EventRegistrationRow[] = [
  {
    id: "r1",
    status: "paid",
    displayName: "Ala",
    email: "ala@t",
    createdAt: "2026-07-13T10:00:00Z",
    paidAt: "2026-07-13T10:05:00Z",
    hasPayment: true,
  },
  {
    id: "r2",
    status: "waitlisted",
    displayName: "Bea",
    email: "bea@t",
    createdAt: "2026-07-13T10:01:00Z",
    waitlistPos: 1,
    hasPayment: false,
  },
  {
    id: "r3",
    status: "refund_requested",
    displayName: "Cez",
    email: "cez@t",
    createdAt: "2026-07-13T10:02:00Z",
    hasPayment: true,
  },
  {
    id: "r4",
    status: "expired",
    displayName: "Dag",
    email: "dag@t",
    createdAt: "2026-07-13T10:03:00Z",
    hasPayment: false,
  },
];
// Reassigned per-test so the status-gating tests can control which rows the
// mocked query returns without a second `vi.mock` module.
let regsData: EventRegistrationRow[] = defaultRows;

vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventRegistrations: () => ({ data: regsData, isPending: false, error: null }),
  useRefundRegistration: () => ({ mutate: refundMutate, error: null, ...refundState }),
  useDenyRefund: () => ({ mutate: denyMutate, error: null, ...denyState }),
  useRemoveRegistration: () => ({ mutate: removeMutate, error: null, ...removeState }),
}));

import type { EventSummary } from "../api";
import { RegistrationsTable } from "./RegistrationsTable";

afterEach(() => {
  cleanup();
  refundState.isPending = false;
  delete refundState.variables;
  denyState.isPending = false;
  delete denyState.variables;
  removeState.isPending = false;
  delete removeState.variables;
  regsData = defaultRows;
});

function renderTable(status: EventSummary["status"] = "published", rows?: EventRegistrationRow[]) {
  if (rows) regsData = rows;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" status={status} />
    </QueryClientProvider>,
  );
}

let nextRowId = 100;
function row(
  overrides: Partial<EventRegistrationRow> & {
    status: EventRegistrationRow["status"];
    displayName: string;
  },
): EventRegistrationRow {
  return {
    id: `row-${nextRowId++}`,
    email: `${overrides.displayName.toLowerCase()}@t`,
    createdAt: "2026-07-13T10:00:00Z",
    hasPayment: false,
    ...overrides,
  };
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

test("a removed registration renders the localized label, not the raw status", () => {
  renderTable("published", [...defaultRows, row({ status: "removed", displayName: "Ewa" })]);
  expect(screen.getByText("Removed")).toBeInTheDocument();
  expect(screen.queryByText("removed")).not.toBeInTheDocument();
});

test("refund flows through the confirm dialog and closes once the mutation settles", async () => {
  renderTable();
  await userEvent.click(screen.getAllByRole("button", { name: "Refund" })[1]!);
  expect(await screen.findByText(/Refund Cez's entry fee\?/)).toBeInTheDocument();
  // The dialog's action button is the last "Refund" in the DOM.
  const buttons = screen.getAllByRole("button", { name: "Refund" });
  await userEvent.click(buttons[buttons.length - 1]!);
  expect(refundMutate).toHaveBeenCalledWith(
    "r3",
    expect.objectContaining({ onSettled: expect.any(Function) }),
  );
  // The dialog must stay open right after mutate() — closing only once the
  // mutation settles is what keeps the confirm button's spinner visible.
  expect(screen.getByText(/Refund Cez's entry fee\?/)).toBeInTheDocument();
  act(() => {
    refundMutate.mock.calls[0]![1].onSettled();
  });
  expect(screen.queryByText(/Refund Cez's entry fee\?/)).not.toBeInTheDocument();
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

test("started shows the refund queue and the paid roster, nothing else", () => {
  // paid (Ala) + waitlisted (Bea) + refund_requested (Cez) + expired (Dag)
  renderTable("started");
  expect(screen.getByText("Cez")).toBeInTheDocument();
  // The paid group outlives `published` so a no-show can still be removed —
  // that is only ever discovered after the event starts.
  expect(screen.getByText("Ala")).toBeInTheDocument();
  expect(screen.queryByText("Bea")).not.toBeInTheDocument();
  expect(screen.queryByText("Dag")).not.toBeInTheDocument();
});

test("started renders the paid roster read-only: Remove, no Refund", () => {
  renderTable("started", [row({ status: "paid", displayName: "Ala", hasPayment: true })]);
  const alaRow = screen.getByText("Ala").closest("li");
  expect(alaRow).not.toBeNull();
  expect(
    within(alaRow as HTMLElement).getByRole("button", { name: m.regs_remove_keep() }),
  ).toBeInTheDocument();
  expect(
    within(alaRow as HTMLElement).queryByRole("button", { name: m.regs_refund() }),
  ).not.toBeInTheDocument();
});

test("started keeps the refund queue's own Refund and Deny buttons", () => {
  renderTable("started", [
    row({ status: "refund_requested", displayName: "Cez", hasPayment: true }),
  ]);
  const cezRow = screen.getByText("Cez").closest("li");
  expect(cezRow).not.toBeNull();
  expect(
    within(cezRow as HTMLElement).getByRole("button", { name: m.regs_refund() }),
  ).toBeInTheDocument();
  expect(
    within(cezRow as HTMLElement).getByRole("button", { name: m.regs_deny() }),
  ).toBeInTheDocument();
});

test("started warns that removing also drops the player from the tournament", async () => {
  const user = userEvent.setup();
  renderTable("started", [row({ status: "paid", displayName: "Ala", hasPayment: true })]);
  await user.click(screen.getByRole("button", { name: m.regs_remove_keep() }));
  expect(screen.getByText(new RegExp(m.regs_remove_drops_from_tournament()))).toBeInTheDocument();
});

test("published does not warn about the tournament — nobody is paired yet", async () => {
  const user = userEvent.setup();
  renderTable("published", [row({ status: "paid", displayName: "Ala", hasPayment: true })]);
  await user.click(screen.getByRole("button", { name: m.regs_remove_keep() }));
  expect(
    screen.queryByText(new RegExp(m.regs_remove_drops_from_tournament())),
  ).not.toBeInTheDocument();
});

test("finished with an empty queue renders nothing", () => {
  regsData = [defaultRows[0]!]; // paid only
  renderTable("finished");
  expect(screen.queryByRole("heading", { name: m.regs_title() })).not.toBeInTheDocument();
  expect(screen.queryByText("Ala")).not.toBeInTheDocument();
});

test("free rows get Remove, paid rows get Refund plus Remove-no-refund", async () => {
  renderTable("published", [
    row({ status: "paid", displayName: "Ann", hasPayment: false }),
    row({ status: "paid", displayName: "Bob", hasPayment: true }),
  ]);
  await screen.findByText("Ann");

  const annRow = screen.getByText("Ann").closest("li");
  const bobRow = screen.getByText("Bob").closest("li");
  expect(annRow).not.toBeNull();
  expect(bobRow).not.toBeNull();

  expect(within(annRow as HTMLElement).getByRole("button", { name: "Remove" })).toBeInTheDocument();
  expect(
    within(annRow as HTMLElement).queryByRole("button", { name: "Refund" }),
  ).not.toBeInTheDocument();

  expect(within(bobRow as HTMLElement).getByRole("button", { name: "Refund" })).toBeInTheDocument();
  expect(
    within(bobRow as HTMLElement).getByRole("button", { name: "Remove — no refund" }),
  ).toBeInTheDocument();
});
