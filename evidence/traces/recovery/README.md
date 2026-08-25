# Recovery fixture traces

Isolated restore of `internal/domain/testdata/duo-db-export.sql` into a
temp `$XDG_DATA_HOME/duo/duo.db`. Injected pane-absent validator — not a
live Herdr dial, and not the live user store under `~/.local/share/duo/`.

`fixture-reconcile.json` is the `session.reconcile` envelope from
`TestFixtureReconcilePaneAbsent`.
