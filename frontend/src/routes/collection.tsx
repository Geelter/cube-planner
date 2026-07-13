import { createFileRoute } from "@tanstack/react-router";
import { CollectionPage } from "@/features/collection/components/CollectionPage";

export const Route = createFileRoute("/collection")({ component: CollectionPage });
