# Fixtures — manual testing

Tiny by design. Duo doesn't ship test data of its own; it operates
against whatever Solo has registered. These fixtures cover the
two files Duo itself reads at startup.

## Files

- [`duo.config.yaml`](./duo.config.yaml) — minimal config. Wires
  Duo to a local Solo binary over stdio. Used by all runbook
  steps. The shipped values include a placeholder path that **must
  be edited** before Duo can spawn Solo. See `00-setup.md` §4.

- [`duo.policy.yaml`](./duo.policy.yaml) — every override block
  exercised by `03-policy-overrides.md`, all commented out by
  default. Uncomment one section at a time, restart Duo, exercise,
  re-comment before moving on. The runbook's policy assertions
  assume only one block is active at a time.

## Why fixtures stay small

Hypomnema's manual-testing fixtures include two indexed Markdown
vaults because its surface includes search and indexing. Duo has no
analogous data — its inputs are the **agent tools registered in
Solo**, which live outside this repo. That's why "fixture
preparation" for Duo is entirely about config files, and why the
runbook leans on the tester to ensure Solo has tools spanning two
or more tiers (see `00-setup.md` §3).
