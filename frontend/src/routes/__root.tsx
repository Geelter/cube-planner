import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
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
    </>
  );
}
