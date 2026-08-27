#!/usr/bin/env python3
"""Read-only dump of duo.domain.fact/v1 stream_log payloads from duo.db."""

import json
import sqlite3
import sys


def main() -> None:
    if len(sys.argv) != 2:
        print("usage: dump-store.py <duo.db>", file=sys.stderr)
        sys.exit(2)

    db_path = sys.argv[1]
    try:
        conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    except sqlite3.Error as exc:
        print(f"dump-store: cannot open {db_path}: {exc}", file=sys.stderr)
        sys.exit(2)

    try:
        rows = conn.execute(
            "SELECT payload FROM stream_log "
            "WHERE stream = 'duo.domain.fact/v1' ORDER BY seq"
        ).fetchall()
    except sqlite3.Error as exc:
        print(f"dump-store: query failed: {exc}", file=sys.stderr)
        sys.exit(2)
    finally:
        conn.close()

    correlations = []
    commands = []
    instance_states = []

    for (payload,) in rows:
        try:
            fact = json.loads(payload)
        except json.JSONDecodeError:
            continue

        kind = fact.get("kind", "")
        if kind == "correlation.claimed":
            corr = fact.get("correlation") or {}
            ext_kind = corr.get("external_kind")
            if ext_kind in ("agent.session", "transcript"):
                correlations.append(
                    {
                        "kind": kind,
                        "session_id": fact.get("session_id"),
                        "instance_id": fact.get("instance_id"),
                        "correlation": corr,
                    }
                )
        elif kind.startswith("command."):
            cmd = fact.get("command") or {}
            attempt = cmd.get("attempt") or {}
            commands.append(
                {
                    "kind": kind,
                    "command": {
                        "id": cmd.get("id"),
                        "state": cmd.get("state"),
                        "path_kind": cmd.get("path_kind"),
                        "attempt": {
                            "id": attempt.get("id"),
                            "path_kind": attempt.get("path_kind"),
                            "recorded_result": attempt.get("recorded_result"),
                        },
                    },
                }
            )
        elif kind == "instance.state":
            instance_states.append(
                {
                    "id": fact.get("id"),
                    "instance_id": fact.get("instance_id"),
                    "state": fact.get("state"),
                    "reason": fact.get("reason"),
                    "evidence": fact.get("evidence"),
                }
            )

    print("## correlations")
    for row in correlations:
        print(json.dumps(row, separators=(",", ":")))

    print("## commands")
    for row in commands:
        print(json.dumps(row, separators=(",", ":")))

    print("## instance.state")
    for row in instance_states:
        print(json.dumps(row, separators=(",", ":")))


if __name__ == "__main__":
    main()
