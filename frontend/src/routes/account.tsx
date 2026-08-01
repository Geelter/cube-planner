import { createFileRoute } from "@tanstack/react-router";
import { requireAuth } from "@/features/auth/guard";
import { AccountPage } from "@/features/auth/components/AccountPage";

export const Route = createFileRoute("/account")({
  beforeLoad: requireAuth,
  component: AccountPage,
});
