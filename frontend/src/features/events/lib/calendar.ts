// Client-side "add to calendar" builders (issue #13). Events are auth-gated,
// so subscription URLs are impossible — a Google template link and a
// downloaded .ics are the two platform-blessed flows. Events store no end
// time; a fixed 4h block was adjudicated in the 2026-08-04 spec.
export type CalendarEvent = {
  id: string;
  name: string;
  description: string;
  location: string;
  startsAt: string;
};

export const EVENT_DURATION_HOURS = 4;

// 2026-08-15T18:00:00+02:00 → 20260815T160000Z
function utcStamp(d: Date): string {
  return d
    .toISOString()
    .replace(/[-:]/g, "")
    .replace(/\.\d{3}Z$/, "Z");
}

function eventTimes(e: CalendarEvent): { start: string; end: string } {
  const start = new Date(e.startsAt);
  const end = new Date(start.getTime() + EVENT_DURATION_HOURS * 60 * 60 * 1000);
  return { start: utcStamp(start), end: utcStamp(end) };
}

export function googleCalendarUrl(e: CalendarEvent): string {
  const { start, end } = eventTimes(e);
  const params = new URLSearchParams({
    action: "TEMPLATE",
    text: e.name,
    dates: `${start}/${end}`,
  });
  if (e.description !== "") params.set("details", e.description);
  if (e.location !== "") params.set("location", e.location);
  return `https://calendar.google.com/calendar/render?${params.toString()}`;
}

// RFC 5545 §3.3.11 TEXT escaping — backslash first.
function escapeIcsText(s: string): string {
  return s
    .replaceAll("\\", "\\\\")
    .replaceAll(";", "\\;")
    .replaceAll(",", "\\,")
    .replace(/\r?\n/g, "\\n");
}

const encoder = new TextEncoder();

// RFC 5545 §3.1: lines fold at 75 octets; continuations start with a space.
function foldIcsLine(line: string): string {
  if (encoder.encode(line).length <= 75) return line;
  const parts: string[] = [];
  let current = "";
  let currentBytes = 0;
  for (const ch of line) {
    const chBytes = encoder.encode(ch).length;
    const limit = parts.length === 0 ? 75 : 74; // continuations lose one octet to the space
    if (currentBytes + chBytes > limit) {
      parts.push(current);
      current = "";
      currentBytes = 0;
    }
    current += ch;
    currentBytes += chBytes;
  }
  parts.push(current);
  return parts.join("\r\n ");
}

export function buildIcs(e: CalendarEvent): string {
  const { start, end } = eventTimes(e);
  const lines = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Cube Planner//cubeplanner.pl//EN",
    "BEGIN:VEVENT",
    `UID:event-${e.id}@cubeplanner.pl`,
    `DTSTAMP:${start}`,
    `DTSTART:${start}`,
    `DTEND:${end}`,
    `SUMMARY:${escapeIcsText(e.name)}`,
  ];
  if (e.description !== "") lines.push(`DESCRIPTION:${escapeIcsText(e.description)}`);
  if (e.location !== "") lines.push(`LOCATION:${escapeIcsText(e.location)}`);
  lines.push("END:VEVENT", "END:VCALENDAR");
  return lines.map(foldIcsLine).join("\r\n") + "\r\n";
}

export function icsFilename(name: string): string {
  const slug =
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "event";
  return `${slug}.ics`;
}
