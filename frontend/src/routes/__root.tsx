import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useEffect, useRef, useState } from "react";
import { useLogout, useMe } from "@/features/auth/api";
import { m } from "@/paraglide/messages";
import { LanguageSwitcher } from "@/shared/i18n/LanguageSwitcher";
import { Button } from "@/shared/ui/button";
import { Drawer } from "@/shared/ui/drawer";
import { ThemeToggle } from "@/shared/ui/theme-toggle";

export const Route = createRootRoute({ component: RootLayout });

const drawerItem = "flex h-12 items-center rounded-md px-3 text-fg hover:bg-surface-raised";

export function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  const mainRef = useRef<HTMLElement>(null);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const firstRender = useRef(true);
  const [menuOpen, setMenuOpen] = useState(false);

  // A11y: move focus to the page content on route change so screen readers
  // announce the new page instead of staying on the clicked link.
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    mainRef.current?.focus();
  }, [pathname]);

  // Close the mobile drawer whenever the route changes.
  useEffect(() => {
    setMenuOpen(false);
  }, [pathname]);

  return (
    <div className="min-h-svh">
      <header className="border-b border-border bg-surface-raised">
        <div className="mx-auto flex h-14 max-w-4xl items-center justify-between gap-4 px-4">
          <div className="flex items-center gap-4">
            <Link to="/" className="font-semibold text-fg hover:text-accent">
              {m.app_name()}
            </Link>
            <nav className="hidden items-center gap-4 md:flex">
              <Link to="/cards" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_cards()}
              </Link>
              <Link to="/cubes" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_cubes()}
              </Link>
              <Link to="/events" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_events()}
              </Link>
            </nav>
          </div>
          <div className="flex items-center gap-2">
            <div className="hidden items-center gap-2 md:flex">
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
            </div>
            <ThemeToggle />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label={m.nav_menu()}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen(true)}
            >
              ☰
            </Button>
          </div>
        </div>
      </header>
      <Drawer open={menuOpen} onClose={() => setMenuOpen(false)} label={m.nav_menu()}>
        <nav className="flex flex-col">
          <Link to="/cards" className={drawerItem}>
            {m.nav_cards()}
          </Link>
          <Link to="/cubes" className={drawerItem}>
            {m.nav_cubes()}
          </Link>
          <Link to="/events" className={drawerItem}>
            {m.nav_events()}
          </Link>
        </nav>
        <hr className="border-border" />
        {me.data ? (
          <div className="flex flex-col">
            <Link to="/cubes/mine" className={drawerItem}>
              {m.cubes_mine_title()}
            </Link>
            <Link to="/collection" className={drawerItem}>
              {m.nav_collection()}
            </Link>
            <Link to="/account" className={drawerItem}>
              {me.data.displayName}
            </Link>
            <Button
              type="button"
              variant="ghost"
              className="h-12 justify-start px-3 text-base font-normal"
              onClick={() => logout.mutate()}
            >
              {m.nav_logout()}
            </Button>
          </div>
        ) : (
          <Link to="/login" className={drawerItem}>
            {m.nav_login()}
          </Link>
        )}
        <hr className="border-border" />
        <div className="px-3 py-2">
          <LanguageSwitcher />
        </div>
      </Drawer>
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
