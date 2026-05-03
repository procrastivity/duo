# Backlog

Use this file for candidate items not yet committed to an active roadmap step.

## Format

- `ID` (optional)
- `Candidate`
- `Why now / why later`
- `Links` (specs, issues, docs)

## Conventions

- When an item lands in a roadmap or is applied, mark it shipped in place by wrapping the candidate label in `~~strikethrough~~` and adding a lifecycle annotation such as **Pulled into round N** or **Shipped <date>**. Strikethrough-in-place is the default because it preserves historical context; outright removal is acceptable only when the item is genuinely obsolete and not worth a historical breadcrumb.
- Live (un-shipped) items have no strikethrough and no lifecycle annotation. Anything with strikethrough or a "Pulled into round N" / "Shipped" annotation is **done** — do not surface it as a candidate for a future round.
- Items can stay in this file indefinitely — un-scoped is a valid state.

## Items

- Candidate: duo packaging — npm esbuild bundle (Channel 1)
  - Why now / why later: foundation for the Nix flake channel; smallest blast radius and useful on its own (faster cold start, smaller install)
  - Links: notes/proposals/duo-packaging-npm-bundle.md, ~/.claude/plans/i-want-to-better-peppy-shamir.md
- Candidate: duo packaging — Bun-compiled macOS binaries (Channel 2)
  - Why now / why later: enables the "no Node required" install path; strict prerequisite for the curl|sh installer and Homebrew tap; independent of Channel 1
  - Links: notes/proposals/duo-packaging-bun-binaries.md, ~/.claude/plans/i-want-to-better-peppy-shamir.md
- Candidate: duo packaging — Nix flake `packages.duo` (Channel 3)
  - Why now / why later: orthogonal to Channel 2; depends on Channel 1's bundled artifact (`dist/duo.mjs`) — schedule after Channel 1 ships
  - Links: notes/proposals/duo-packaging-nix-flake.md, ~/.claude/plans/i-want-to-better-peppy-shamir.md
- Candidate: duo packaging — install UX (GitHub Releases / curl|sh / Homebrew tap) (Channel 4)
  - Why now / why later: depends on Channel 2 binary artifacts existing with stable filenames; staged rollout (a) Releases polish → (b) curl|sh → (c) Homebrew tap, ordered by maintenance cost
  - Links: notes/proposals/duo-packaging-install-ux.md, ~/.claude/plans/i-want-to-better-peppy-shamir.md
- ID: solo-todo-223
  Candidate: Customizable grouping names and selection criteria for model tiers
  - Why now / why later: extends the small/medium/large tier system from the prior (placeholder) Roadmap 1 step 2 work — users want to add tiers (e.g. `[xs,s,m,l,xl]`, `[cheap,standard,fancy]`) and customize which models map to each tier. Defer until packaging channels stabilize and a real adoption signal arrives; touches config schema design, validation, and backwards-compat with default tier names — risks becoming a config-file nightmare without that signal.
  - Open questions (from todo): ordered vs unordered tier labels; migration/aliasing of existing `small/medium/large` references; expression complexity for selection criteria vs simple match-by-attribute.
  - Links: solo://proj/6/todo/customizable-groupin--223
