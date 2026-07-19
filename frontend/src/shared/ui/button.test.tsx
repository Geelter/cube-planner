import { render, screen } from "@testing-library/react";
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
});
