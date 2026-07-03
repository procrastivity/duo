import { describe, expect, it } from "vitest";

import { tokenizeArgs } from "./tokenize-args.js";

describe("tokenizeArgs", () => {
  it("returns [] for empty input", () => {
    expect(tokenizeArgs("")).toEqual([]);
  });

  it("returns [] for whitespace-only input", () => {
    expect(tokenizeArgs("   \t \n ")).toEqual([]);
  });

  it("splits on runs of mixed whitespace", () => {
    expect(tokenizeArgs("a b\tc")).toEqual(["a", "b", "c"]);
    expect(tokenizeArgs("  a   b  ")).toEqual(["a", "b"]);
    expect(tokenizeArgs("a\nb\rc")).toEqual(["a", "b", "c"]);
  });

  it("passes through flags and values verbatim", () => {
    expect(tokenizeArgs("--model sonnet --seed 42")).toEqual([
      "--model",
      "sonnet",
      "--seed",
      "42",
    ]);
  });

  it("groups a double-quoted run into one token", () => {
    expect(tokenizeArgs('--prompt "hello world"')).toEqual([
      "--prompt",
      "hello world",
    ]);
  });

  it("groups a single-quoted run into one token, literally", () => {
    expect(tokenizeArgs("--prompt 'hello world'")).toEqual([
      "--prompt",
      "hello world",
    ]);
  });

  it("treats backslashes as literal inside single quotes", () => {
    expect(tokenizeArgs("'a\\b'")).toEqual(["a\\b"]);
    expect(tokenizeArgs("'it\\'")).toEqual(["it\\"]);
  });

  it("escapes only \" and \\ inside double quotes", () => {
    expect(tokenizeArgs('"a\\"b"')).toEqual(['a"b']);
    expect(tokenizeArgs('"a\\\\b"')).toEqual(["a\\b"]);
    // A backslash before any other char is retained (POSIX behavior).
    expect(tokenizeArgs('"a\\nb"')).toEqual(["a\\nb"]);
  });

  it("escapes the next character with a backslash outside quotes", () => {
    expect(tokenizeArgs("a\\ b")).toEqual(["a b"]);
    expect(tokenizeArgs("a\\\\b")).toEqual(["a\\b"]);
    expect(tokenizeArgs('\\"quoted\\"')).toEqual(['"quoted"']);
  });

  it("joins adjacent quoted and bare runs into one token", () => {
    expect(tokenizeArgs('foo"bar baz"qux')).toEqual(["foobar bazqux"]);
    expect(tokenizeArgs("a'b c'd")).toEqual(["ab cd"]);
  });

  it("preserves empty quoted runs as empty-string tokens", () => {
    expect(tokenizeArgs('""')).toEqual([""]);
    expect(tokenizeArgs("''")).toEqual([""]);
    expect(tokenizeArgs('a "" b')).toEqual(["a", "", "b"]);
  });

  it("does NOT expand env vars, globs, or command substitution", () => {
    expect(tokenizeArgs("$HOME *.ts `id`")).toEqual([
      "$HOME",
      "*.ts",
      "`id`",
    ]);
    expect(tokenizeArgs('"$HOME/*.ts"')).toEqual(["$HOME/*.ts"]);
  });

  it("tolerates an unterminated quote by closing at end of input", () => {
    expect(tokenizeArgs('a "unterminated')).toEqual(["a", "unterminated"]);
    expect(tokenizeArgs("a 'unterminated")).toEqual(["a", "unterminated"]);
  });

  it("keeps a trailing backslash literal", () => {
    expect(tokenizeArgs("a\\")).toEqual(["a\\"]);
  });
});
