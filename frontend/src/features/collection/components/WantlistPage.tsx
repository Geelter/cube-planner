import { getRouteApi, Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { UnauthorizedError, useWantlist } from "../api";
import { downloadTextFile, wantlistFilename, wantlistToCardmarketText } from "../lib/cardmarket";

const route = getRouteApi("/cubes/$cubeId/wantlist");

export function WantlistPage() {
  const { cubeId } = route.useParams();
  const wantlist = useWantlist(cubeId);

  if (wantlist.isPending) return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (wantlist.isError) {
    if (wantlist.error instanceof UnauthorizedError) {
      return (
        <Alert variant="default">
          {wantlist.error.message}{" "}
          <Link to="/login" className="font-medium underline">
            {m.nav_login()}
          </Link>
        </Alert>
      );
    }
    return <Alert variant="danger">{wantlist.error.message}</Alert>;
  }

  const { cubeName, items, totalMissing } = wantlist.data;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">{m.wantlist_title({ cube: cubeName })}</h1>
          <p className="text-sm text-fg-muted">{m.wantlist_total({ count: totalMissing })}</p>
        </div>
        {items.length > 0 && (
          <Button
            type="button"
            onClick={() =>
              downloadTextFile(wantlistFilename(cubeName), wantlistToCardmarketText(items))
            }
          >
            {m.wantlist_download()}
          </Button>
        )}
      </div>

      {items.length === 0 ? (
        <p className="text-sm text-fg-muted">{m.wantlist_empty()}</p>
      ) : (
        <table className="w-full max-w-2xl text-sm">
          <thead>
            <tr className="border-b border-border text-left text-fg-muted">
              <th scope="col" className="py-1.5 font-medium">
                {m.wantlist_col_card()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_missing()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_in_cube()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_owned()}
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.oracleId} className="border-b border-border">
                {/* CardHoverPreview's hover/focus handlers make the linter treat this
                    cell as a control needing a label; the visible card name is the
                    label, the rule just can't see through the custom component. */}
                {/* eslint-disable-next-line jsx-a11y/control-has-associated-label */}
                <td className="py-1.5">
                  <CardHoverPreview card={item}>
                    <span className="text-fg">{item.name}</span>
                  </CardHoverPreview>
                </td>
                <td className="py-1.5 text-right font-semibold text-accent tabular-nums">
                  {item.missingQuantity}
                </td>
                <td className="py-1.5 text-right text-fg-muted tabular-nums">
                  {item.cubeQuantity}
                </td>
                <td className="py-1.5 text-right text-fg-muted tabular-nums">
                  {item.ownedQuantity}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
