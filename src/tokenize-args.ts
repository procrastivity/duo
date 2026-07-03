/**
 * Split a raw `extra_args` string into an argv array (D3 / OQ3).
 *
 * POSIX-ish, deliberately minimal — this is a word-splitter, NOT a shell:
 * - Whitespace (space, tab, newline, …) delimits tokens.
 * - Single quotes group a run literally; nothing inside is special (not even a
 *   backslash), up to the closing `'`.
 * - Double quotes group a run; inside, a backslash escapes only `"` and `\`
 *   (any other backslash is kept verbatim, matching POSIX).
 * - Outside quotes, a backslash escapes the next character literally.
 * - NO environment/glob/command expansion — `$FOO`, `*`, and `` `cmd` `` are
 *   passed through as ordinary characters. This is intentional: it keeps the
 *   util dependency-free and free of any shell-injection surface.
 * - An empty or whitespace-only input yields `[]`. An empty quoted run (`""` or
 *   `''`) yields a single empty-string token.
 * - An unterminated quote is tolerated: end-of-input closes the current token.
 */
export const tokenizeArgs = (raw: string): string[] => {
  const tokens: string[] = [];
  let current = "";
  // Distinguishes an empty token that was explicitly quoted ("" / '') from the
  // gap between whitespace-separated tokens.
  let hasToken = false;

  const pushToken = (): void => {
    if (hasToken) {
      tokens.push(current);
    }
    current = "";
    hasToken = false;
  };

  let i = 0;
  const n = raw.length;
  while (i < n) {
    const ch = raw[i]!;

    if (ch === "'") {
      // Single-quoted run: everything literal until the next single quote.
      hasToken = true;
      i += 1;
      while (i < n && raw[i] !== "'") {
        current += raw[i];
        i += 1;
      }
      i += 1; // consume closing quote (or run past end if unterminated)
      continue;
    }

    if (ch === '"') {
      // Double-quoted run: backslash escapes only `"` and `\`.
      hasToken = true;
      i += 1;
      while (i < n && raw[i] !== '"') {
        if (raw[i] === "\\" && i + 1 < n && (raw[i + 1] === '"' || raw[i + 1] === "\\")) {
          current += raw[i + 1];
          i += 2;
        } else {
          current += raw[i];
          i += 1;
        }
      }
      i += 1; // consume closing quote (or run past end if unterminated)
      continue;
    }

    if (ch === "\\") {
      // Escape the next character literally; a trailing backslash is literal.
      if (i + 1 < n) {
        current += raw[i + 1];
        hasToken = true;
        i += 2;
      } else {
        current += ch;
        hasToken = true;
        i += 1;
      }
      continue;
    }

    if (ch === " " || ch === "\t" || ch === "\n" || ch === "\r" || ch === "\f" || ch === "\v") {
      pushToken();
      i += 1;
      continue;
    }

    current += ch;
    hasToken = true;
    i += 1;
  }

  pushToken();
  return tokens;
};
