import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { client } from "@/shared/api/client";

const route = getRouteApi("/verify-email");

export function VerifyEmailPage() {
  const { token } = route.useSearch();
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

  if (!token) return <p role="alert">Missing verification token.</p>;
  if (verify.isPending || verify.isIdle) return <p>Verifying…</p>;
  if (verify.isError) return <p role="alert">{verify.error.message}</p>;
  return (
    <main>
      <h1>Email verified</h1>
      <p>
        You can now <Link to="/login">log in</Link>.
      </p>
    </main>
  );
}
