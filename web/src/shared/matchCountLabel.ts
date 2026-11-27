import { pluralCount } from "./plural";

// The count under a search box's results, shared so every search list words
// it the same. Results are cut at a page; past it, the hint says to narrow.
export function matchCountLabel(total: number, shown: number): string {
  if (total > shown) return `${shown} of ${total} matches · keep typing to narrow`;
  return pluralCount(total, "match", "matches");
}
