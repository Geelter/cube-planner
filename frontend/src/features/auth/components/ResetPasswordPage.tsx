import { getRouteApi, Link } from "@tanstack/react-router";
import { useState } from "react";
import { client } from "@/shared/api/client";

const route = getRouteApi("/reset-password");

export function ResetPasswordPage() {
  const { token } = route.useSearch();
  const [password, setPassword] = useState("");
  const [state, setState] = useState<"idle" | "done" | "error">("idle");
  const [message, setMessage] = useState("");

  if (!token) return <p role="alert">Missing reset token.</p>;
  if (state === "done") {
    return (
      <main>
        <p>
          Password updated. <Link to="/login">Log in</Link>.
        </p>
      </main>
    );
  }
  return (
    <main>
      <h1>Reset password</h1>
      {state === "error" && <p role="alert">{message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void (async () => {
            const { error } = await client.POST("/api/auth/reset-password", {
              body: { token, newPassword: password },
            });
            if (error) {
              setState("error");
              setMessage(error.detail ?? "reset failed");
            } else {
              setState("done");
            }
          })();
        }}
      >
        <label>
          New password (min 8 characters)
          <input
            type="password"
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit">Set new password</button>
      </form>
    </main>
  );
}
