import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { client } from "../api/client";

export const Route = createFileRoute("/register")({ component: RegisterPage });

function RegisterPage() {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState<"idle" | "sent" | "error">("idle");
  const [message, setMessage] = useState("");

  if (status === "sent") {
    return (
      <main>
        <h1>Check your inbox</h1>
        <p>We sent a verification link to {email}.</p>
      </main>
    );
  }

  return (
    <main>
      <h1>Register</h1>
      {status === "error" && <p role="alert">{message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void (async () => {
            const { error } = await client.POST("/api/auth/register", {
              body: { email, displayName, password },
            });
            if (error) {
              setStatus("error");
              setMessage(error.detail ?? "registration failed");
            } else {
              setStatus("sent");
            }
          })();
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Display name
          <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} required />
        </label>
        <label>
          Password (min 8 characters)
          <input
            type="password"
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit">Register</button>
      </form>
    </main>
  );
}
