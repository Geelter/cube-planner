import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useRegister } from "../api";

export function RegisterPage() {
  const register = useRegister();
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");

  if (register.isSuccess) {
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
          {register.isError && <Alert variant="danger">{register.error.message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              register.mutate({ email, displayName, password });
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
            <Button type="submit" loading={register.isPending}>
              {m.register_submit()}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
