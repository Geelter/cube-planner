import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";

const route = getRouteApi("/verify-email");

export function VerifyEmailPage() {
  const { token } = route.useSearch();
  const verify = useMutation({
    mutationFn: async (t: string) => {
      const { error } = await client.POST("/api/auth/verify-email", { body: { token: t } });
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
  });
  const mutate = verify.mutate;
  const fired = useRef(false);

  useEffect(() => {
    if (!token || fired.current) return;
    fired.current = true;
    mutate(token);
  }, [token, mutate]);

  const wrap = "mx-auto w-full max-w-sm";
  if (!token) {
    return (
      <div className={wrap}>
        <Alert variant="danger">{m.verify_missing_token()}</Alert>
      </div>
    );
  }
  if (verify.isPending || verify.isIdle) {
    return (
      <div className={wrap}>
        <p className="text-sm text-fg-muted">{m.verify_pending()}</p>
      </div>
    );
  }
  if (verify.isError) {
    return (
      <div className={wrap}>
        <Alert variant="danger">{verify.error.message}</Alert>
      </div>
    );
  }
  return (
    <div className={wrap}>
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.verify_done_title()}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm">
            {m.verify_done_body()}{" "}
            <Link className="text-accent hover:underline" to="/login">
              {m.nav_login()}
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
