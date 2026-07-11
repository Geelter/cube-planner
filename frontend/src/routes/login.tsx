import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useLogin } from "../api/auth";

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>): { error?: string } =>
    typeof s["error"] === "string" ? { error: s["error"] } : {},
  component: LoginPage,
});

function LoginPage() {
  const { error } = Route.useSearch();
  const login = useLogin();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  return (
    <main>
      <h1>Log in</h1>
      {error === "oauth" && <p role="alert">Social login failed. Try again.</p>}
      {error === "email-taken" && (
        <p role="alert">
          That email is already registered. Log in and link accounts from your account page.
        </p>
      )}
      {error !== undefined && error !== "oauth" && error !== "email-taken" && (
        <p role="alert">Something went wrong. Try again.</p>
      )}
      {login.isError && <p role="alert">{login.error.message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          login.mutate({ email, password }, { onSuccess: () => void navigate({ to: "/" }) });
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit" disabled={login.isPending}>
          Log in
        </button>
      </form>
      <p>
        <a href="/auth/oauth/discord/start">Log in with Discord</a> ·{" "}
        <a href="/auth/oauth/google/start">Log in with Google</a>
      </p>
      <p>
        <Link to="/register">Register</Link> · <Link to="/forgot-password">Forgot password?</Link>
      </p>
    </main>
  );
}
