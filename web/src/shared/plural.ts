// Every count in the UI picks its word one way: the singular for exactly one,
// so "0 plans" and "1 plan". The word alone is for a count set apart from it.
export function pluralWord(n: number, one: string, many: string): string {
  return n === 1 ? one : many;
}

export function pluralCount(n: number, one: string, many: string): string {
  return `${n} ${pluralWord(n, one, many)}`;
}
