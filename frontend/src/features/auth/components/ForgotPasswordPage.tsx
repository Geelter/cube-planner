import { useState } from "react";
import { client } from "@/shared/api/client";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <main>
        <p>If an account exists for {email}, a reset link is on its way.</p>
      </main>
    );
  }
  return (
    <main>
      <h1>Forgot password</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void client
            .POST("/api/auth/forgot-password", { body: { email } })
            .then(() => setSent(true));
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <button type="submit">Send reset link</button>
      </form>
    </main>
  );
}
