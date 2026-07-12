import { createFileRoute } from "@tanstack/react-router";
import { CardSearchPage } from "@/features/cards/components/CardSearchPage";

export const Route = createFileRoute("/cards")({
  component: CardSearchPage,
});
