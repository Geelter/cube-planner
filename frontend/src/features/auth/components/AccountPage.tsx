import { Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { useMe } from "../api";

const ALL_PROVIDERS = ["discord", "google"] as const;

export function AccountPage() {
  const me = useMe();

  if (me.isPending) return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (!me.data) {
    return (
      <p className="text-sm">
        {m.account_not_logged_in()}{" "}
        <Link className="text-accent hover:underline" to="/login">
          {m.nav_login()}
        </Link>
      </p>
    );
  }
  const linked = new Set(me.data.providers ?? []);
  return (
    <div className="mx-auto w-full max-w-md">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.account_title()}</CardTitle>
          <p className="text-sm text-fg-muted">
            {me.data.displayName} — {me.data.email}
          </p>
        </CardHeader>
        <CardContent>
          <h2 className="mb-2 text-sm font-semibold">{m.account_linked_title()}</h2>
          <ul className="flex flex-col gap-1 text-sm">
            {ALL_PROVIDERS.map((p) => (
              <li key={p} className="capitalize">
                {p}:{" "}
                {linked.has(p) ? (
                  m.account_linked()
                ) : (
                  <a
                    className="text-accent normal-case hover:underline"
                    href={`/auth/oauth/${p}/start?link=1`}
                  >
                    {m.account_link_now()}
                  </a>
                )}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
