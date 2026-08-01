import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";
import { meQueryOptions } from "./api";

/**
 * beforeLoad guard for routes that require a session. Reads the shared
 * "me" query through the cache (ensureQueryData), so navigation doesn't
 * refetch when the session state is already known. Ownership/role checks
 * stay server-side — this only requires *a* logged-in user.
 */
export async function requireAuth({
  context,
}: {
  context: { queryClient: QueryClient };
}): Promise<void> {
  const me = await context.queryClient.ensureQueryData(meQueryOptions);
  if (!me) throw redirect({ to: "/login" });
}
