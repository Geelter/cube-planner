import { createFileRoute, Link } from "@tanstack/react-router";
import { useMe } from "../api/auth";

export const Route = createFileRoute("/account")({ component: AccountPage });

const ALL_PROVIDERS = ["discord", "google"] as const;

function AccountPage() {
  const me = useMe();

  if (me.isPending) return <p>Loading…</p>;
  if (!me.data) {
    return (
      <p>
        You are not logged in. <Link to="/login">Log in</Link>
      </p>
    );
  }
  const linked = new Set(me.data.providers ?? []);
  return (
    <main>
      <h1>Account</h1>
      <p>
        {me.data.displayName} — {me.data.email}
      </p>
      <h2>Linked logins</h2>
      <ul>
        {ALL_PROVIDERS.map((p) => (
          <li key={p}>
            {p}: {linked.has(p) ? "linked" : <a href={`/auth/oauth/${p}/start?link=1`}>link now</a>}
          </li>
        ))}
      </ul>
    </main>
  );
}
