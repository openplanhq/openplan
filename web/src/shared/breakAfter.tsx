import { Fragment } from "react";

// A machine value (a repository, a path, a type expression) cut into the
// pieces it may wrap between: each piece ends with one of `separators`, so a
// line can end after "/" or "(" but never inside a name. Joined back, the
// pieces are the value.
export function piecesAfter(value: string, separators: string): string[] {
  const pieces: string[] = [];
  let start = 0;
  for (let i = 0; i < value.length; i++) {
    if (separators.includes(value[i]) && i + 1 < value.length) {
      pieces.push(value.slice(start, i + 1));
      start = i + 1;
    }
  }
  pieces.push(value.slice(start));
  return pieces;
}

/** The pieces of a value with a <wbr> after each, so it wraps only there. */
export function Breakable({ pieces }: { pieces: string[] }) {
  return (
    <>
      {pieces.map((piece, index) => (
        <Fragment key={index}>
          {piece}
          {index < pieces.length - 1 && <wbr />}
        </Fragment>
      ))}
    </>
  );
}
