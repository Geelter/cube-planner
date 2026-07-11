import { createFileRoute } from "@tanstack/react-router";
import { LoginPage } from "@/features/auth/components/LoginPage";

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>): { error?: string } =>
    typeof s["error"] === "string" ? { error: s["error"] } : {},
  component: LoginPage,
});
