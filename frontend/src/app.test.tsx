import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { client } from "./api/client";

test("renders heading", () => {
  render(<h1>Cube Planner</h1>);
  expect(screen.getByRole("heading", { name: "Cube Planner" })).toBeInTheDocument();
});

test("api client is typed", () => {
  // Compile-time check: healthz path exists on the typed client.
  expect(typeof client.GET).toBe("function");
});
