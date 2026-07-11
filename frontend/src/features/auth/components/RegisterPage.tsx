import { useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

export function RegisterPage() {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState<"idle" | "sent" | "error">("idle");
  const [message, setMessage] = useState("");

  if (status === "sent") {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardHeader>
            <CardTitle as="h1">{m.register_sent_title()}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-fg-muted">{m.register_sent_body({ email })}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.register_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {status === "error" && <Alert variant="danger">{message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void (async () => {
                const { error } = await client.POST("/api/auth/register", {
                  body: { email, displayName, password },
                });
                if (error) {
                  setStatus("error");
                  setMessage(error.detail ?? m.register_error_fallback());
                } else {
                  setStatus("sent");
                }
              })();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-email">{m.field_email()}</Label>
              <Input
                id="register-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-display-name">{m.field_display_name()}</Label>
              <Input
                id="register-display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-password">{m.field_password_min()}</Label>
              <Input
                id="register-password"
                type="password"
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit">{m.register_submit()}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
