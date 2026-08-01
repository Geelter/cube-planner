import { getRouteApi, Link } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useResetPassword } from "../api";

const route = getRouteApi("/reset-password");

export function ResetPasswordPage() {
  const reset = useResetPassword();
  const { token } = route.useSearch();
  const [password, setPassword] = useState("");

  if (!token) {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Alert variant="danger">{m.reset_missing_token()}</Alert>
      </div>
    );
  }
  if (reset.isSuccess) {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm">
              {m.reset_done()}{" "}
              <Link className="text-accent hover:underline" to="/login">
                {m.nav_login()}
              </Link>
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.reset_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {reset.isError && <Alert variant="danger">{reset.error.message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              reset.mutate({ token, newPassword: password });
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="reset-password">{m.field_new_password_min()}</Label>
              <Input
                id="reset-password"
                type="password"
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit" loading={reset.isPending}>
              {m.reset_submit()}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
