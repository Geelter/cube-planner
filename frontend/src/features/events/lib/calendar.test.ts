import { describe, expect, test } from "vitest";
import { buildIcs, googleCalendarUrl, icsFilename } from "./calendar";

const event = {
  id: "11111111-2222-3333-4444-555555555555",
  name: "Summer Draft; Finals",
  description: "Bring snacks, and sleeves.\nDoors open early.",
  location: "Community Hall, Kraków",
  startsAt: "2026-08-15T16:00:00Z",
};

describe("googleCalendarUrl", () => {
  test("builds a template link with UTC start/end 4h apart", () => {
    const url = new URL(googleCalendarUrl(event));
    expect(url.origin + url.pathname).toBe("https://calendar.google.com/calendar/render");
    expect(url.searchParams.get("action")).toBe("TEMPLATE");
    expect(url.searchParams.get("text")).toBe("Summer Draft; Finals");
    expect(url.searchParams.get("dates")).toBe("20260815T160000Z/20260815T200000Z");
    expect(url.searchParams.get("details")).toBe(event.description);
    expect(url.searchParams.get("location")).toBe(event.location);
  });

  test("omits empty details and location", () => {
    const url = new URL(googleCalendarUrl({ ...event, description: "", location: "" }));
    expect(url.searchParams.has("details")).toBe(false);
    expect(url.searchParams.has("location")).toBe(false);
  });
});

describe("buildIcs", () => {
  test("emits a valid VEVENT with UTC times, 4h duration, CRLF endings", () => {
    const ics = buildIcs(event);
    expect(ics).toContain("BEGIN:VCALENDAR\r\n");
    expect(ics).toContain("UID:event-11111111-2222-3333-4444-555555555555@cubeplanner.pl\r\n");
    expect(ics).toContain("DTSTART:20260815T160000Z\r\n");
    expect(ics).toContain("DTEND:20260815T200000Z\r\n");
    expect(ics.endsWith("END:VCALENDAR\r\n")).toBe(true);
    expect(ics.includes("\n") && !ics.includes("\r\n")).toBe(false); // CRLF only
  });

  test("escapes RFC 5545 TEXT characters", () => {
    const ics = buildIcs(event);
    expect(ics).toContain("SUMMARY:Summer Draft\\; Finals");
    expect(ics).toContain("DESCRIPTION:Bring snacks\\, and sleeves.\\nDoors open early.");
    expect(ics).toContain("LOCATION:Community Hall\\, Kraków");
  });

  test("folds lines longer than 75 octets with a leading space", () => {
    const ics = buildIcs({ ...event, description: "x".repeat(200) });
    const folded = ics.split("\r\n").filter((l) => l.startsWith(" "));
    expect(folded.length).toBeGreaterThan(0);
    for (const line of ics.split("\r\n")) {
      expect(new TextEncoder().encode(line).length).toBeLessThanOrEqual(75);
    }
  });

  test("omits DESCRIPTION and LOCATION when empty", () => {
    const ics = buildIcs({ ...event, description: "", location: "" });
    expect(ics).not.toContain("DESCRIPTION:");
    expect(ics).not.toContain("LOCATION:");
  });
});

test("icsFilename slugifies", () => {
  expect(icsFilename("Summer Draft; Finals")).toBe("summer-draft-finals.ics");
});
