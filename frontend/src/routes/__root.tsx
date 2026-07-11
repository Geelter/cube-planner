import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useLogout, useMe } from "../api/auth";

export const Route = createRootRoute({ component: RootLayout });

function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  return (
    <>
      <nav>
        <Link to="/">Cube Planner</Link>{" "}
        {me.data ? (
          <>
            <Link to="/account">{me.data.displayName}</Link>{" "}
            <button type="button" onClick={() => logout.mutate()}>
              Log out
            </button>
          </>
        ) : (
          <Link to="/login">Log in</Link>
        )}
      </nav>
      <Outlet />
      {import.meta.env.DEV && (
        <TanStackDevtools
          plugins={[
            { name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> },
            { name: "TanStack Query", render: <ReactQueryDevtoolsPanel /> },
          ]}
        />
      )}
    </>
  );
}
