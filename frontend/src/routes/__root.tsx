import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useEffect, useRef } from "react";
import { useLogout, useMe } from "@/features/auth/api";
import { m } from "@/paraglide/messages";
import { LanguageSwitcher } from "@/shared/i18n/LanguageSwitcher";
import { Button } from "@/shared/ui/button";
import { ThemeToggle } from "@/shared/ui/theme-toggle";

export const Route = createRootRoute({ component: RootLayout });

function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  const mainRef = useRef<HTMLElement>(null);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const firstRender = useRef(true);

  // A11y: move focus to the page content on route change so screen readers
  // announce the new page instead of staying on the clicked link.
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    mainRef.current?.focus();
  }, [pathname]);

  return (
    <div className="min-h-svh">
      <header className="border-b border-border bg-surface-raised">
        <div className="mx-auto flex h-14 max-w-4xl items-center justify-between gap-4 px-4">
          <nav className="flex items-center gap-4">
            <Link to="/" className="font-semibold text-fg hover:text-accent">
              {m.app_name()}
            </Link>
            <Link to="/cards" className="text-sm text-fg-muted hover:text-fg">
              {m.nav_cards()}
            </Link>
            <Link to="/cubes" className="text-sm text-fg-muted hover:text-fg">
              {m.nav_cubes()}
            </Link>
          </nav>
          <div className="flex items-center gap-2">
            {me.data ? (
              <>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/cubes/mine">{m.cubes_mine_title()}</Link>
                </Button>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/collection">{m.nav_collection()}</Link>
                </Button>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/account">{me.data.displayName}</Link>
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={() => logout.mutate()}>
                  {m.nav_logout()}
                </Button>
              </>
            ) : (
              <Button asChild variant="outline" size="sm">
                <Link to="/login">{m.nav_login()}</Link>
              </Button>
            )}
            <LanguageSwitcher />
            <ThemeToggle />
          </div>
        </div>
      </header>
      <main ref={mainRef} tabIndex={-1} className="mx-auto max-w-4xl px-4 py-8 outline-none">
        <Outlet />
      </main>
      {import.meta.env.DEV && (
        <TanStackDevtools
          plugins={[
            { name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> },
            { name: "TanStack Query", render: <ReactQueryDevtoolsPanel /> },
          ]}
        />
      )}
    </div>
  );
}
