# Dogfood day log — 2026-08-24

Config authored and validated this day (step 24, with the user).
Binary: duo built from `go` @ c534067 (--output chassis-wide).

| capture | verb / claim |
|---|---|
| 01-doctor.txt | doctor: config recognized as duo.config/v3 at the default path; ambient host deduction visible |
| 02-dry-runs.txt | launch --dry-run: all four presets resolve; build_and_verify honors distinct_model_family |
| 03-dry-run-json.txt | launch --dry-run --output json: duo.external/v1 envelope |
| duo.config.yaml | the authored document (copy) |

Checkpoint verbs still to exercise live: launch (real), list, show,
detach, reattach, authority-restart recovery.
