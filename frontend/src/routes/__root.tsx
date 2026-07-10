import { createRootRoute, Link, Outlet } from "@tanstack/react-router";

export const Route = createRootRoute({
  component: () => (
    <>
      <nav>
        <Link to="/">Cube Planner</Link>
      </nav>
      <Outlet />
    </>
  ),
});
