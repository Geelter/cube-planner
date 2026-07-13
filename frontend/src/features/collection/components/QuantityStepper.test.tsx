import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { QuantityStepper } from "./QuantityStepper";

// shouldAdvanceTime: userEvent v14's internal pointer/click handling awaits
// its own timers, which deadlocks against a fully-frozen fake clock. Letting
// real time trickle the fake clock forward keeps userEvent responsive while
// vi.advanceTimersByTime(400) below still drives the debounce deterministically.
beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

test("rapid clicks land as ONE commit with the final value", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Lightning Bolt" quantity={4} onCommit={onCommit} />);

  const inc = screen.getByRole("button", { name: "Increase quantity of Lightning Bolt" });
  await user.click(inc);
  await user.click(inc);
  await user.click(inc);
  expect(screen.getByText("7")).toBeInTheDocument(); // optimistic display
  expect(onCommit).not.toHaveBeenCalled();

  vi.advanceTimersByTime(400);
  expect(onCommit).toHaveBeenCalledTimes(1);
  expect(onCommit).toHaveBeenCalledWith(7);
});

test("decrementing to zero commits zero (remove path)", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Sol Ring" quantity={1} onCommit={onCommit} />);

  await user.click(screen.getByRole("button", { name: "Decrease quantity of Sol Ring" }));
  vi.advanceTimersByTime(400);
  expect(onCommit).toHaveBeenCalledWith(0);
});

test("no commit when the value returns to the server quantity", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Sol Ring" quantity={2} onCommit={onCommit} />);

  await user.click(screen.getByRole("button", { name: "Increase quantity of Sol Ring" }));
  await user.click(screen.getByRole("button", { name: "Decrease quantity of Sol Ring" }));
  vi.advanceTimersByTime(400);
  expect(onCommit).not.toHaveBeenCalled();
});
