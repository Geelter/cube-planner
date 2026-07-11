import { createFileRoute } from "@tanstack/react-router";
import { AccountPage } from "@/features/auth/components/AccountPage";

export const Route = createFileRoute("/account")({ component: AccountPage });
