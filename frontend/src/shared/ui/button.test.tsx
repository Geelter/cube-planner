import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Button } from "./button";

describe("Button", () => {
  it("renders an accessible button with variant classes", () => {
    render(<Button variant="danger">Delete</Button>);
    const btn = screen.getByRole("button", { name: "Delete" });
    expect(btn.className).toContain("bg-danger");
  });

  it("lets consumer className win over defaults via cn()", () => {
    render(<Button className="h-12">Tall</Button>);
    const btn = screen.getByRole("button", { name: "Tall" });
    expect(btn.className).toContain("h-12");
    expect(btn.className).not.toContain("h-9");
  });

  it("renders as the child element with asChild", () => {
    render(
      <Button asChild>
        <a href="/x">Go</a>
      </Button>,
    );
    expect(screen.getByRole("link", { name: "Go" })).toBeInTheDocument();
  });

  it("renders the lg touch size", () => {
    render(<Button size="lg">Report</Button>);
    const btn = screen.getByRole("button", { name: "Report" });
    expect(btn.className).toContain("h-11");
  });

  it("shows a spinner and disables the button while loading", () => {
    render(<Button loading>Save</Button>);
    // The spinner's aria-label joins the accessible name, so match loosely.
    const btn = screen.getByRole("button", { name: /Save/ });
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
    expect(within(btn).getByRole("status")).toBeInTheDocument();
  });

  it("renders no spinner when not loading", () => {
    render(<Button>Save</Button>);
    const btn = screen.getByRole("button", { name: "Save" });
    expect(btn).not.toBeDisabled();
    expect(btn).not.toHaveAttribute("aria-busy");
    expect(within(btn).queryByRole("status")).toBeNull();
  });
});
