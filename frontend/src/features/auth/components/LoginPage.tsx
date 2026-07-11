import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useLogin } from "../api";

const route = getRouteApi("/login");

export function LoginPage() {
  const { error } = route.useSearch();
  const login = useLogin();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.login_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {error === "oauth" && <Alert variant="danger">{m.login_error_oauth()}</Alert>}
          {error === "email-taken" && <Alert variant="danger">{m.login_error_email_taken()}</Alert>}
          {error !== undefined && error !== "oauth" && error !== "email-taken" && (
            <Alert variant="danger">{m.error_generic()}</Alert>
          )}
          {login.isError && <Alert variant="danger">{login.error.message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              login.mutate({ email, password }, { onSuccess: () => void navigate({ to: "/" }) });
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-email">{m.field_email()}</Label>
              <Input
                id="login-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-password">{m.field_password()}</Label>
              <Input
                id="login-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit" disabled={login.isPending}>
              {m.login_submit()}
            </Button>
          </form>
          <p className="text-sm text-fg-muted">
            <a className="text-accent hover:underline" href="/auth/oauth/discord/start">
              {m.login_with_discord()}
            </a>
            {" · "}
            <a className="text-accent hover:underline" href="/auth/oauth/google/start">
              {m.login_with_google()}
            </a>
          </p>
          <p className="text-sm text-fg-muted">
            <Link className="text-accent hover:underline" to="/register">
              {m.login_register_link()}
            </Link>
            {" · "}
            <Link className="text-accent hover:underline" to="/forgot-password">
              {m.login_forgot_link()}
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
