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
};
const names = new Map([
  ["pl1", "Ann"],
  ["pl2", "Bob"],
]);

function renderForm(onSubmit = vi.fn()) {
  render(
    <ResultForm
      match={match}
      playerNames={names}
      onSubmit={onSubmit}
      pending={false}
      error={null}
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
  expect(onSubmit).toHaveBeenCalledWith({ p1Games: 2, p2Games: 1, draws: 0 });
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
