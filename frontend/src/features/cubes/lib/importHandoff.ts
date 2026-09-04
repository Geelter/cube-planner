import type { ResolvedItem } from "@/shared/cards/CardListImportDialog";

// Carries a resolved paste-a-list result from cube creation to the editor
// through TanStack Router's navigation `state`, not a query string (a
// 500-card list would not fit in a URL). `location.state` is typed through
// the ambient `HistoryState` interface (see @tanstack/history), which any
// consumer may augment — this is that augmentation, shared by
// CreateCubePage (writer) and CubeEditorPage (one-time reader, which then
// clears it with a replace navigation so a refresh or Back does not
// re-stage it).
declare module "@tanstack/history" {
  interface HistoryState {
    importedItems?: ResolvedItem[];
  }
}
