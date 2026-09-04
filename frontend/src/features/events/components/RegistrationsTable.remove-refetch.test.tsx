// Exercises the real `useRemoveRegistration` / `useEventRegistrations`
// hooks (unlike RegistrationsTable.test.tsx, which mocks the whole "../api"
// module) so the mutation's own `onSettled` invalidation actually runs.
// This is the regression test for the 409 dead-end: a stale cached
// `hasPayment` causes `remove-needs-decision`, and the row must refetch to
// show the now-correct buttons instead of stranding the organizer on a
// button that will just 409 again.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { RegistrationsTable } from "./RegistrationsTable";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function renderReal() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" status="published" />
    </QueryClientProvider>,
  );
}

test("a 409 remove-needs-decision refetches so the row's buttons update", async () => {
  const staleRow = {
    id: "r1",
    status: "paid",
    displayName: "Ann",
    email: "ann@t",
    createdAt: "2026-07-13T10:00:00Z",
    hasPayment: false,
  };
  // A payment webhook landed between page load and click: the server now
  // sees a payment intent on the row, which is exactly why it 409s.
  const freshRow = { ...staleRow, hasPayment: true };

  let registrationsCalls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      if (input.method === "GET" && url.pathname.endsWith("/registrations")) {
        registrationsCalls += 1;
        const row = registrationsCalls === 1 ? staleRow : freshRow;
        return jsonResponse({ registrations: [row] });
      }
      if (input.method === "POST" && url.pathname.endsWith("/remove")) {
        return jsonResponse(
          {
            type: "remove-needs-decision",
            title: "Conflict",
            status: 409,
            detail: "paid registration needs a refund decision",
          },
          409,
        );
      }
      throw new Error(`unexpected fetch ${input.method} ${url.pathname}`);
    }),
  );

  renderReal();
  await screen.findByText("Ann");

  // The stale cache says hasPayment: false, so only the plain Remove
  // button is offered.
  expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Refund" })).not.toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Remove" }));
  const confirmButtons = await screen.findAllByRole("button", { name: "Remove" });
  await userEvent.click(confirmButtons[confirmButtons.length - 1]!);

  // Once the 409 lands, the mutation's onSettled invalidation must
  // refetch the row — surfacing the real Refund / Remove-no-refund
  // buttons instead of leaving the stale Remove button in place.
  await waitFor(() => expect(registrationsCalls).toBe(2));
  await waitFor(() =>
    expect(screen.getByRole("button", { name: "Remove — no refund" })).toBeInTheDocument(),
  );
  expect(screen.getByRole("button", { name: "Refund" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();
});
