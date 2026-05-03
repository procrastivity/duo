import { stderr, stdout } from "node:process";

export interface OutputOptions {
  json?: boolean;
  quiet?: boolean;
  noColor?: boolean;
}

const colorEnabled = (opts: OutputOptions): boolean => {
  if (opts.noColor) return false;
  if (process.env.NO_COLOR) return false;
  if (!stdout.isTTY) return false;
  return true;
};

export const colorize = (
  text: string,
  code: string,
  opts: OutputOptions = {},
): string => (colorEnabled(opts) ? `\x1b[${code}m${text}\x1b[0m` : text);

export const green = (s: string, o?: OutputOptions) => colorize(s, "32", o);
export const red = (s: string, o?: OutputOptions) => colorize(s, "31", o);
export const yellow = (s: string, o?: OutputOptions) => colorize(s, "33", o);
export const dim = (s: string, o?: OutputOptions) => colorize(s, "2", o);

export interface Column<Row> {
  header: string;
  get: (row: Row) => string;
  truncate?: number;
}

const truncate = (s: string, max?: number): string => {
  if (max === undefined || s.length <= max) return s;
  if (max <= 1) return s.slice(0, max);
  return s.slice(0, max - 1) + "…";
};

export const renderTable = <Row>(rows: readonly Row[], cols: readonly Column<Row>[]): string => {
  if (rows.length === 0) return "";
  const widths = cols.map((c) => c.header.length);
  const cells: string[][] = rows.map((row) =>
    cols.map((c, i) => {
      const v = truncate(c.get(row), c.truncate);
      if (v.length > widths[i]!) widths[i] = v.length;
      return v;
    }),
  );
  const lines: string[] = [];
  lines.push(cols.map((c, i) => c.header.padEnd(widths[i]!)).join("  "));
  for (const row of cells) {
    lines.push(row.map((v, i) => v.padEnd(widths[i]!)).join("  "));
  }
  return lines.map((l) => l.trimEnd()).join("\n");
};

export const writeOut = (s: string): void => {
  stdout.write(s.endsWith("\n") ? s : s + "\n");
};

export const writeErr = (s: string): void => {
  stderr.write(s.endsWith("\n") ? s : s + "\n");
};

export const writeJson = (data: unknown): void => {
  stdout.write(JSON.stringify(data, null, 2) + "\n");
};

export const printResult = <Row>(
  rows: readonly Row[],
  cols: readonly Column<Row>[],
  opts: OutputOptions & { quietField?: (row: Row) => string },
): void => {
  if (opts.json) {
    writeJson(rows);
    return;
  }
  if (opts.quiet) {
    const f = opts.quietField ?? ((r: Row) => String((r as { id?: unknown }).id ?? ""));
    for (const r of rows) writeOut(f(r));
    return;
  }
  if (rows.length === 0) {
    writeOut("(no results)");
    return;
  }
  writeOut(renderTable(rows, cols));
};

export const printObject = (
  data: Record<string, unknown>,
  opts: OutputOptions,
): void => {
  if (opts.json) {
    writeJson(data);
    return;
  }
  if (opts.quiet) {
    return;
  }
  const keyWidth = Math.max(...Object.keys(data).map((k) => k.length));
  for (const [k, v] of Object.entries(data)) {
    writeOut(`${k.padEnd(keyWidth)}  ${formatValue(v)}`);
  }
};

const formatValue = (v: unknown): string => {
  if (v === undefined || v === null) return "—";
  if (typeof v === "string") return v;
  return JSON.stringify(v);
};
