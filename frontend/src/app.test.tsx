import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

test("renders heading", () => {
  render(<h1>Cube Planner</h1>);
  expect(screen.getByRole("heading", { name: "Cube Planner" })).toBeInTheDocument();
});
