import type { InfiniteData } from "@tanstack/react-query";
import type { ListPage } from "./types";

// Test support for lists. Every list endpoint answers {items, next_cursor},
// and nothing else the API sends is an array, so a test's fetch mock can give
// a list's items and have them sent as the one page they make.
export function asListBody(body: unknown): unknown {
  return Array.isArray(body) ? listPage(body) : body;
}

export function listPage<T>(items: T[], nextCursor: string | null = null): ListPage<T> {
  return { items, next_cursor: nextCursor };
}

/** A paged list's cache entry holding items as its one loaded page. */
export function pagesOf<T>(items: T[]): InfiniteData<ListPage<T>, string | undefined> {
  return { pages: [listPage(items)], pageParams: [undefined] };
}
