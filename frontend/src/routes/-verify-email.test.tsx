import { QueryClient, QueryClientProvider, useMutation } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { StrictMode, useEffect, useRef } from "react";
import { expect, test, vi } from "vitest";
import { client } from "../api/client";

// verify-email.tsx is a route component (createFileRoute), which requires a
// full router context to render. Rather than standing up a router in this
// test, we exercise the same effect + mutation shape the component uses,
// wrapped in StrictMode, to prove the fire-once guard prevents a double
// network call when React double-invokes effects in development.
function VerifyEmailProbe({ token }: { token: string }) {
  const verify = useMutation({
    mutationFn: async (t: string) => {
      const { error } = await client.POST("/api/auth/verify-email", { body: { token: t } });
      if (error) throw new Error(error.detail ?? "verification failed");
    },
  });
  const mutate = verify.mutate;
  const fired = useRef(false);

  useEffect(() => {
    if (!token || fired.current) return;
    fired.current = true;
    mutate(token);
  }, [token, mutate]);

  if (verify.isPending || verify.isIdle) return <p>Verifying…</p>;
  if (verify.isError) return <p role="alert">{verify.error.message}</p>;
  return <p>Email verified</p>;
}

test("fire-once guard calls verify-email endpoint exactly once under StrictMode double-invoke", async () => {
  const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchMock);

  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <StrictMode>
      <QueryClientProvider client={qc}>
        <VerifyEmailProbe token="abc123" />
      </QueryClientProvider>
    </StrictMode>,
  );

  await waitFor(() => expect(fetchMock).toHaveBeenCalled());
  // Give any second (buggy) invocation a chance to fire before asserting.
  await new Promise((resolve) => setTimeout(resolve, 50));
  expect(fetchMock).toHaveBeenCalledTimes(1);

  vi.unstubAllGlobals();
});
