#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
summary="$root/probe-summary.json"

jq -e '
  .schema == "duo.portable-launcher-step07-evidence/v1" and
  .observed_at_utc.start[0:10] == "2026-09-18" and
  .observed_at_utc.finish[0:10] == "2026-09-18" and
  .amp.version == "0.0.1789715829-g82b7fb" and
  .amp.executable_sha256 == "sha256:36b6da094db6e984ede80f65027674bda3fd1c126828b1fb4c254c003b6cd568" and
  .amp.matches_step01_pin == false and
  .amp.matches_manifest_tested_version == false and
  .scope_result.step07 == "blocked" and
  (.scope_result.blocker | contains("accepted Amp 0.0.1789675234-g2899fe executable was not available")) and
  .scope_result.full_amp_suite_claimed == false and
  .scope_result.launcher_support_claimed == false and
  .duo.commit == .checkout.commit and
  .skill.name == .amp_discovery.name and
  .skill.installed_root == ($__step07_run + "/workspace/.agents/skills/duo-delegation-loop") and
  .amp_discovery.source == "workspace-agents" and
  .amp_discovery.loader_invoked == true and
  .amp_discovery.model_turn_invoked == false and
  .amp_discovery.threads_created == false and
  .installation.state == "current" and
  .installation.target == "portable_launchers" and
  .positive_preflight.schema == "duo.launcher-preflight/v1" and
  .positive_preflight.status == "ready" and
  .positive_preflight.authority_scope == "local" and
  ([.positive_preflight.checks[] | select(.required == true and .status == "pass" and .code == "ok")] | length) == 7 and
  ([.positive_preflight.checks[] | select(.id == "host_compatibility" and .required == false and .status == "warning" and .code == "host.compatibility_unverified")] | length) == 1 and
  .negative_preflight.status == "not_ready" and
  .negative_preflight.authority_scope == "unavailable" and
  .negative_preflight.failed_check.stage == "host_reachability" and
  .negative_preflight.failed_check.code == "host.unreachable" and
  (.negative_preflight.failed_check.action | contains("$RUN/xdg/runtime/no-server/herdr.sock")) and
  .negative_preflight.duo_authority_entries_before == 0 and
  .negative_preflight.duo_authority_entries_after == 0 and
  .negative_preflight.workspace_payload_manifest_sha256_before == .negative_preflight.workspace_payload_manifest_sha256_after and
  .negative_preflight.launch_command_invoked == false and
  .negative_preflight.authority_store_created == false and
  .negative_preflight.workspace_changed == false and
  .isolation.plugin_start_events == 0 and
  .isolation.mcp_configured == false and
  .isolation.terminal_input_used == false and
  .scrub.status == "pass"
' --arg __step07_run '$RUN' "$summary" >/dev/null

if grep -En '/tmp/duo-step07|sgamp_|Bearer[[:space:]]|user_code|"access_token"|"refresh_token"' "$summary" "$root/report.md"; then
  echo "step-07 evidence scrub check failed" >&2
  exit 1
fi

printf '%s\n' "step-07 evidence validation: pass"
