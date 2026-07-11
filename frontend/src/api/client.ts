import createClient from "openapi-fetch";
import type { paths } from "./schema";

// openapi-fetch builds a `Request` before invoking `fetch`, and Node's
// global `Request` (unlike a real browser) requires an absolute URL even
// though `window.location` reports one. Anchor to the current origin so
// relative paths keep working the same way in the browser and under
// Vitest/jsdom.
//
// `fetch` is also wrapped instead of passed directly: openapi-fetch
// captures its `fetch` option once, at `createClient()` time, as a default
// parameter (`fetch: baseFetch = globalThis.fetch`). Since `client` is a
// module-level singleton, that capture happens at import time, before any
// test can call `vi.stubGlobal("fetch", ...)`. Indirecting through a
// wrapper defers the `globalThis.fetch` lookup to each request instead.
export const client = createClient<paths>({
  baseUrl: window.location.origin,
  fetch: (input: Request) => globalThis.fetch(input),
});
