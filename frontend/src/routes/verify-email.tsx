import { createFileRoute } from "@tanstack/react-router";
import { VerifyEmailPage } from "@/features/auth/components/VerifyEmailPage";

export const Route = createFileRoute("/verify-email")({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s["token"] === "string" ? s["token"] : "",
  }),
  component: VerifyEmailPage,
});
