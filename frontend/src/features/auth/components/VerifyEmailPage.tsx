import { getRouteApi, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";

const route = getRouteApi("/verify-email");

// The request fires once on mount (the token is single-use) and is tracked
// with component state rather than useMutation: under StrictMode's simulated
// remount, a mutation fired from the first effect pass lands on the discarded
// observer while the fired-ref guard (correctly) stops the retained one from
// re-firing — so the page would sit on "Verifying…" forever
// (TanStack/query#5341, closed wontfix). setState from the first pass's
// promise reaches the surviving instance; the fired ref still guarantees the
// token is consumed exactly once.
type VerifyState = { status: "pending" | "success" } | { status: "error"; message: string };

export function VerifyEmailPage() {
  const { token } = route.useSearch();
  const [verify, setVerify] = useState<VerifyState>({ status: "pending" });
  const fired = useRef(false);

  useEffect(() => {
    if (!token || fired.current) return;
    fired.current = true;
    client
      .POST("/api/auth/verify-email", { body: { token } })
      .then(({ error }) => {
        if (error) setVerify({ status: "error", message: error.detail ?? m.error_generic() });
        else setVerify({ status: "success" });
      })
      .catch(() => setVerify({ status: "error", message: m.error_generic() }));
  }, [token]);

  const wrap = "mx-auto w-full max-w-sm";
  if (!token) {
    return (
      <div className={wrap}>
        <Alert variant="danger">{m.verify_missing_token()}</Alert>
      </div>
    );
  }
  if (verify.status === "pending") {
    return (
      <div className={wrap}>
        <p className="text-sm text-fg-muted">{m.verify_pending()}</p>
      </div>
    );
  }
  if (verify.status === "error") {
    return (
      <div className={wrap}>
        <Alert variant="danger">{verify.message}</Alert>
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
