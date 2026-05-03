import { describe, it, expect } from "vitest";
import { renderTable } from "./output.js";

describe("renderTable", () => {
  it("aligns columns by max width", () => {
    const out = renderTable(
      [{ a: "1", b: "longer" }, { a: "22", b: "x" }],
      [
        { header: "A", get: (r) => r.a },
        { header: "B", get: (r) => r.b },
      ],
    );
    const lines = out.split("\n");
    expect(lines[0]).toBe("A   B");
    expect(lines[1]).toBe("1   longer");
    expect(lines[2]).toBe("22  x");
  });

  it("returns empty string for no rows", () => {
    const out = renderTable([], [{ header: "X", get: () => "" }]);
    expect(out).toBe("");
  });

  it("truncates long fields with ellipsis", () => {
    const out = renderTable(
      [{ a: "abcdefghij" }],
      [{ header: "A", get: (r) => r.a, truncate: 5 }],
    );
    expect(out.split("\n")[1]).toBe("abcd…");
  });
});
