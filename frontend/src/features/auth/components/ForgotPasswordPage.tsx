import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useForgotPassword } from "../api";

export function ForgotPasswordPage() {
  const forgot = useForgotPassword();
  const [email, setEmail] = useState("");

  if (forgot.isSuccess) {
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
              forgot.mutate({ email });
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
            <Button type="submit" loading={forgot.isPending}>
              {m.forgot_submit()}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
