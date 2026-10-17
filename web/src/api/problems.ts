import { ApiRequestError } from "./client";

// fieldErrors picks the field errors under one JSON Pointer prefix, keyed by
// the rest of the pointer: with prefix "#/config/", "#/config/region" becomes
// "region". A form uses it to put each error on its own field. The pointer is
// URI-fragment encoded with RFC 6901 escapes, so both are undone here.
export function fieldErrors(error: unknown, prefix: string): Record<string, string> {
  const byField: Record<string, string> = {};
  if (!(error instanceof ApiRequestError)) {
    return byField;
  }
  for (const entry of error.errors) {
    if (entry.pointer === undefined || !entry.pointer.startsWith(prefix)) {
      continue;
    }
    const key = decodePointerSegment(entry.pointer.slice(prefix.length));
    // The first error on a field is the one shown; the server lists the most
    // basic problem first.
    byField[key] ??= entry.detail;
  }
  return byField;
}

function decodePointerSegment(segment: string): string {
  let decoded = segment;
  try {
    decoded = decodeURIComponent(segment);
  } catch {
    // A malformed escape is shown as sent rather than dropped.
  }
  return decoded.replace(/~1/g, "/").replace(/~0/g, "~");
}

// fieldsShown says which pointers a form draws on a field: those under prefix
// whose key is one the form renders an input for. An error on any other key,
// such as a variable the form doesn't show, must stay in the summary line or
// it would show nowhere.
export function fieldsShown(prefix: string, keys: readonly string[]): (pointer: string) => boolean {
  const rendered = new Set(keys);
  return (pointer) => pointer.startsWith(prefix) && rendered.has(decodePointerSegment(pointer.slice(prefix.length)));
}

// apiErrorMessage is the one line a screen shows for a failed request.
// - Field errors the screen draws on their fields (those shownInline accepts)
//   are left out; the rest are listed, since a summary like "2 fields need
//   attention" says nothing about which. With every one drawn inline it is "".
// - A server fault carries the request id, so a person can quote it.
export function apiErrorMessage(error: unknown, fallback: string, shownInline?: (pointer: string) => boolean): string {
  if (!(error instanceof ApiRequestError)) {
    return error instanceof Error && error.message !== "" ? error.message : fallback;
  }
  if (error.errors.length > 0) {
    return error.errors
      .filter((entry) => entry.pointer === undefined || shownInline === undefined || !shownInline(entry.pointer))
      .map((entry) => entry.detail)
      // Details start with the field's name, which is case-sensitive, so
      // they are joined as they are rather than capitalized into sentences.
      .join("; ");
  }
  const message = error.message || fallback;
  if (error.status >= 500 && error.requestId !== "") {
    return `${message} Request ID: ${error.requestId}.`;
  }
  return message;
}
