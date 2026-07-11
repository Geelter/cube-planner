import { useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-fg-muted">{m.forgot_sent({ email })}</p>
          </CardContent>
        </Card>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.forgot_title()}</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void client
                .POST("/api/auth/forgot-password", { body: { email } })
                .then(() => setSent(true));
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="forgot-email">{m.field_email()}</Label>
              <Input
                id="forgot-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <Button type="submit">{m.forgot_submit()}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
