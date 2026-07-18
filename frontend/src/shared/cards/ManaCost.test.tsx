import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ManaCost } from "./ManaCost";

function symbolImgs(wrapper: HTMLElement) {
  return [...wrapper.querySelectorAll("img")];
}

describe("ManaCost", () => {
  it("renders one img per symbol, in order", () => {
    render(<ManaCost cost="{2}{W}{W}" />);
    const wrapper = screen.getByRole("img", { name: "{2}{W}{W}" });
    const imgs = symbolImgs(wrapper);
    expect(imgs).toHaveLength(3);
    expect(imgs[0]?.src).toContain("/2.svg");
    expect(imgs[1]?.src).toContain("/W.svg");
    expect(imgs[2]?.src).toContain("/W.svg");
  });

  it("resolves hybrid and phyrexian tokens by stripping slashes", () => {
    render(<ManaCost cost="{W/U}{G/W/P}" />);
    const imgs = symbolImgs(screen.getByRole("img", { name: "{W/U}{G/W/P}" }));
    expect(imgs[0]?.src).toContain("/WU.svg");
    expect(imgs[1]?.src).toContain("/GWP.svg");
  });

  it("falls back to literal text for unknown tokens", () => {
    render(<ManaCost cost="{Q}{W}" />);
    const wrapper = screen.getByRole("img", { name: "{Q}{W}" });
    expect(wrapper).toHaveTextContent("{Q}");
    expect(symbolImgs(wrapper)).toHaveLength(1);
  });

  it("preserves text outside braces (split cards)", () => {
    render(<ManaCost cost="{1}{W} // {2}{U}" />);
    const wrapper = screen.getByRole("img", { name: "{1}{W} // {2}{U}" });
    expect(wrapper).toHaveTextContent("//");
    expect(symbolImgs(wrapper)).toHaveLength(4);
  });

  it("renders nothing for an empty cost", () => {
    const { container } = render(<ManaCost cost="" />);
    expect(container).toBeEmptyDOMElement();
  });
});
