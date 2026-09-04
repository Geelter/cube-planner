import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentMatch } from "../api";
import { ResultForm } from "./ResultForm";

afterEach(cleanup);

const match: TournamentMatch = {
  id: "m1",
  tableNumber: 1,
  player1Id: "pl1",
  player2Id: "pl2",
  disputed: false,
  hadDispute: false,
  reports: [],
  resultLocked: false,
};
const names = new Map([
  ["pl1", "Ann"],
  ["pl2", "Bob"],
]);

function renderForm(onSubmit = vi.fn(), onDirtyChange?: (dirty: boolean) => void) {
  render(
    <ResultForm
      match={match}
      playerNames={names}
      onSubmit={onSubmit}
      pending={false}
      error={null}
      // exactOptionalPropertyTypes: only spread the prop in when a handler
      // was actually passed, rather than ever assigning it `undefined`.
      {...(onDirtyChange ? { onDirtyChange } : {})}
    />,
  );
  return onSubmit;
}

test("submits a valid 2-1", async () => {
  const onSubmit = renderForm();
  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  await userEvent.clear(screen.getByLabelText("Bob: games won"));
  await userEvent.type(screen.getByLabelText("Bob: games won"), "1");
  await userEvent.click(screen.getByRole("button", { name: "Report result" }));
  expect(onSubmit).toHaveBeenCalledWith({ p1Games: 2, p2Games: 1 });
});

test("rejects 2-2 with a validation message", async () => {
  const onSubmit = renderForm();
  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  await userEvent.clear(screen.getByLabelText("Bob: games won"));
  await userEvent.type(screen.getByLabelText("Bob: games won"), "2");
  await userEvent.click(screen.getByRole("button", { name: "Report result" }));
  expect(onSubmit).not.toHaveBeenCalled();
  expect(screen.getByRole("alert")).toHaveTextContent("Enter a valid best-of-3 score.");
});

test("has no draws input and hints that equal games are a draw", async () => {
  renderForm();
  expect(screen.queryByLabelText(/draw/i)).not.toBeInTheDocument();

  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "1");
  await userEvent.clear(screen.getByLabelText("Bob: games won"));
  await userEvent.type(screen.getByLabelText("Bob: games won"), "1");
  expect(screen.getByText(/recorded as a draw/i)).toBeInTheDocument();
});

test("reports dirty state as the entered result diverges from the stored one, and clears on unmount", async () => {
  const onDirtyChange = vi.fn();
  renderForm(vi.fn(), onDirtyChange);
  expect(onDirtyChange).toHaveBeenLastCalledWith(false);

  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  expect(onDirtyChange).toHaveBeenLastCalledWith(true);

  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "0");
  expect(onDirtyChange).toHaveBeenLastCalledWith(false);

  cleanup();
  expect(onDirtyChange).toHaveBeenLastCalledWith(false);
});
