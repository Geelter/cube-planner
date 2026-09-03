import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CardAutocomplete } from "./CardAutocomplete";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

test("clears the input and keeps focus after a card is picked", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify({
        cards: [
          {
            scryfallId: "11111111-1111-1111-1111-111111111111",
            oracleId: "22222222-2222-2222-2222-222222222222",
            name: "Lightning Bolt",
            manaCost: "{R}",
            typeLine: "Instant",
            colors: ["R"],
            setCode: "leb",
            setName: "Limited Edition Beta",
            collectorNumber: "162",
            imageSmall: null,
            imageNormal: null,
          },
        ],
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    ),
  );
  const onSelect = vi.fn();
  renderWithClient(<CardAutocomplete id="add" onSelect={onSelect} />);

  const input = screen.getByRole("combobox");
  await userEvent.type(input, "bolt");
  const option = await waitFor(() => screen.getByRole("option", { name: /Lightning Bolt/ }));
  await userEvent.click(option);

  expect(onSelect).toHaveBeenCalledTimes(1);
  expect(input).toHaveValue("");
  expect(input).toHaveFocus();
  // An empty query is below minChars, so no list reopens on refocus.
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});
