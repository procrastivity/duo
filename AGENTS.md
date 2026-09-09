# duo agent instructions

Work is tracked with `wip` (matters, stages, steps).

## Cross-repo coordination (duo ↔ duo-lab)

Duo owns product code and implementation matters. Runtime research —
probes, fixtures, evidence, and the integration matrix — lives in
`~/Code/duo-lab`. Work that belongs to the other side is authored on
the other side, never adopted locally.

**On sealing any matter here (finish or cancel), run a handoff check:**

1. State the cross-repo impact in one of three forms: it unblocks work
   there, it invalidates an assumption there, or it has no impact. Say
   "no cross-repo impact" explicitly — a null result is a result.
2. When it unblocks work there: draft the matter or backlog entry
   (title, body, seal condition, origin reference) and offer it. Author
   it only after the user approves, using wip verbs in that repo.
3. When it invalidates an assumption there (an overturned claim, a
   stale version pin): offer a finding on the affected matter, or a
   backlog entry when no matter exists yet.
4. Do not wait for seal when a finding invalidates the other repo's
   *active* work — surface that immediately.

**Provenance and freshness:**

5. A cross-authored item names its origin: repo, matter locator, and
   version pin. Record the handoff as a finding on the origin matter.
6. Any handoff that rests on a version pin states the pin and its
   date. The consuming matter re-verifies the live version at its
   first step.
7. Research questions discovered in duo go to the duo-lab backlog.
   Implementation work discovered in duo-lab goes to duo.
