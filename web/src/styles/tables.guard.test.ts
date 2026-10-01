import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Table } from "@/components/ui/table";

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

function componentFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return componentFiles(path);
    return entry.name.endsWith(".tsx") && !entry.name.includes(".test.") ? [path] : [];
  });
}

// This list only shrinks as screen PRs move tables to <Table>; the combined
// registry-and-runs migration leaves no legacy tables.
const LEGACY_TABLES: string[] = [];

// Every table has fixed layout, with its column widths declared in a
// <colgroup>. Left to automatic layout, a table re-sizes its columns whenever
// a cell's content changes, so a status that moves from queued to
// waiting_approval shifts every column after it.
describe("tables", () => {
  const files = componentFiles(SRC_DIR)
    .map((path) => ({ name: relative(SRC_DIR, path), source: readFileSync(path, "utf8") }))
    .filter(({ name }) => !name.startsWith(`components${sep}ui${sep}`));

  it("finds the tables it guards", () => {
    expect(files.some(({ source }) => /<Table\b/.test(source))).toBe(true);
  });

  it("builds every table on the shadcn Table", () => {
    const raw = files
      .filter(({ name, source }) => /<table\b/.test(source) && !LEGACY_TABLES.includes(name))
      .map(({ name }) => name);
    expect(raw, `use <Table> from @/components/ui/table in: ${raw.join(", ")}`).toEqual([]);
  });

  it("opens every Table with a <colgroup>", () => {
    const violations = files.flatMap(({ name, source }) =>
      [...source.matchAll(/<Table\b[^>]*>/g)]
        .filter((match) => !source.slice(match.index + match[0].length).trimStart().startsWith("<colgroup>"))
        .map((match) => `${name}: ${match[0]} is not followed by <colgroup>`)
    );
    expect(violations, violations.join("\n")).toEqual([]);
  });

  // cn() merges classes like tailwind-merge, so a caller's table-auto would
  // silently replace table-fixed if the Table let it.
  it("keeps the Table's layout fixed whatever class the caller passes", () => {
    const markup = renderToStaticMarkup(createElement(Table, { className: "table-auto" }));
    const table = markup.match(/<table [^>]*class="([^"]*)"/)?.[1] ?? "";
    expect(table.split(" ")).toContain("table-fixed");
    expect(table.split(" ")).not.toContain("table-auto");
  });

  describe("legacy tables", () => {
    const legacy = files.filter(({ name }) => LEGACY_TABLES.includes(name));

    // The list only shrinks: a file that no longer has a raw table leaves it.
    it("lists only files that still have a raw table", () => {
      expect(legacy.map(({ name }) => name).sort()).toEqual([...LEGACY_TABLES].sort());
      const migrated = legacy.filter(({ source }) => !/<table\b/.test(source)).map(({ name }) => name);
      expect(migrated, `remove from LEGACY_TABLES: ${migrated.join(", ")}`).toEqual([]);
    });

    // Every legacy <table> is a .data-table (primitives.css): fixed layout
    // with its widths in a <colgroup>, inside a .data-table-frame. Each table
    // must open with its own <colgroup>: a count across the file would also
    // count the shadcn <Table>s' and any "<colgroup>" in text.
    it("builds every legacy table on .data-table", () => {
      const violations: string[] = [];
      for (const { name, source } of legacy) {
        const tables = [...source.matchAll(/<table\b([^>]*)>/g)];
        for (const match of tables) {
          const [tag, attributes] = match;
          if (!/className=[{"][^>]*\bdata-table\b/.test(attributes)) violations.push(`${name}: ${tag} lacks the data-table class`);
          if (!source.slice(match.index + tag.length).trimStart().startsWith("<colgroup>")) {
            violations.push(`${name}: ${tag} is not followed by <colgroup>`);
          }
        }
        const frames = source.match(/\bdata-table-frame\b/g)?.length ?? 0;
        if (frames < tables.length) violations.push(`${name}: ${tables.length} table(s) but ${frames} data-table-frame`);
      }
      expect(violations, violations.join("\n")).toEqual([]);
    });
  });
});
