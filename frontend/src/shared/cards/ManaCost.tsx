const symbolUrls = import.meta.glob<string>("./mana/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

type Part = { key: number; symbolUrl: string | null; text: string };

function parseCost(cost: string): Part[] {
  const parts: Part[] = [];
  let last = 0;
  for (const match of cost.matchAll(/\{([^}]+)\}/g)) {
    if (match.index > last) {
      parts.push({ key: parts.length, symbolUrl: null, text: cost.slice(last, match.index) });
    }
    const stem = (match[1] ?? "").toUpperCase().replaceAll("/", "");
    const url = symbolUrls[`./mana/${stem}.svg`];
    parts.push({ key: parts.length, symbolUrl: url ?? null, text: match[0] });
    last = match.index + match[0].length;
  }
  if (last < cost.length) {
    parts.push({ key: parts.length, symbolUrl: null, text: cost.slice(last) });
  }
  return parts;
}

export function ManaCost({ cost }: { cost: string }) {
  if (cost === "") {
    return null;
  }
  return (
    // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
    <span role="img" aria-label={cost}>
      {parseCost(cost).map((part) =>
        part.symbolUrl === null ? (
          <span key={part.key}>{part.text}</span>
        ) : (
          <img
            key={part.key}
            src={part.symbolUrl}
            alt=""
            className="inline-block size-[1em] align-[-0.15em]"
          />
        ),
      )}
    </span>
  );
}
