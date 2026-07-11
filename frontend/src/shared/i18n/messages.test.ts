import { afterEach, describe, expect, it } from "vitest";
import { m } from "@/paraglide/messages";
import { overwriteGetLocale } from "@/paraglide/runtime";

describe("message catalogs", () => {
  afterEach(() => overwriteGetLocale(() => "en"));

  it("renders per-locale copy", () => {
    expect(m.login_title()).toBe("Log in");
    overwriteGetLocale(() => "pl");
    expect(m.login_title()).toBe("Logowanie");
  });

  it("interpolates parameters", () => {
    overwriteGetLocale(() => "pl");
    expect(m.register_sent_body({ email: "a@b.c" })).toContain("a@b.c");
  });
});
