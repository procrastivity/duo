#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
summary="$root/probe-summary.json"

jq -e '
  .schema == "duo.portable-launcher-step09-evidence/v1" and
  .observed_at_utc.start[0:10] == "2026-09-18" and
  .observed_at_utc.finish[0:10] == "2026-09-18" and
  .checkout.commit == .duo.commit and
  .opencode.version == "1.18.31" and
  .opencode.accepted_version == "1.18.31" and
  .opencode.executable_sha256 == "sha256:f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11" and
  .opencode.accepted_executable_sha256 == .opencode.executable_sha256 and
  .opencode.matches_accepted_pin == true and
  .opencode.matches_manifest_tested_version == true and
  .scope_result.step09 == "pass" and
  .scope_result.seal_condition_met == true and
  .scope_result.exact_accepted_launcher_used == true and
  .scope_result.full_opencode_suite_claimed == false and
  .scope_result.launcher_support_claimed == false and
  .installation.schema == "duo.external/v1" and
  .installation.operation == "projection.install" and
  .installation.target == "portable_launchers" and
  .installation.state == "current" and
  .skill.name == .opencode_discovery.name and
  .skill.format_version == "duo.skill/duo-delegation-loop/v1" and
  .skill.content_digest == "sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182" and
  .skill.installed_file == ($__run + "/workspace/.agents/skills/duo-delegation-loop/SKILL.md") and
  .skill.stamp_schema == "duo.projection-stamp/v1" and
  .skill.stamp_target == "portable_launchers" and
  .skill.installed_bytes_match_canonical == true and
  .opencode_discovery.matching_skill_records == 1 and
  .opencode_discovery.location == .skill.installed_file and
  .opencode_discovery.loaded_body_sha256 == .opencode_discovery.canonical_body_sha256 and
  .opencode_discovery.loaded_body_matches_canonical == true and
  .opencode_discovery.body_has_launch_sequence == true and
  .opencode_discovery.body_has_install_guidance == true and
  .opencode_discovery.loader_invoked == true and
  .opencode_discovery.model_turn_invoked == false and
  .opencode_discovery.behavioral_model_adherence_claimed == false and
  .opencode_discovery.sessions_created == 0 and
  .opencode_discovery.accounts_present == 0 and
  .opencode_discovery.credentials_present == 0 and
  .isolation.external_plugins_disabled_by_pure == true and
  (.isolation.resolved_plugins | length) == 0 and
  .isolation.mcp_configured == false and
  .isolation.terminal_input_used == false and
  .isolation.threads_created == false and
  .positive_preflight.schema == "duo.launcher-preflight/v1" and
  .positive_preflight.status == "ready" and
  .positive_preflight.authority_scope == "local" and
  .positive_preflight.host_source == "explicit-flag" and
  ([.positive_preflight.checks[] | select(.required == true and .status == "pass" and .code == "ok")] | length) == 7 and
  ([.positive_preflight.checks[] | select(.id == "host_compatibility" and .required == false and .status == "warning" and .code == "host.compatibility_unverified")] | length) == 1 and
  .positive_preflight.skill_projection_state == "current" and
  .positive_preflight.host_reachable == true and
  .positive_preflight.duo_authority_entries_before == 0 and
  .positive_preflight.duo_authority_entries_after == 0 and
  .positive_preflight.workspace_payload_manifest_sha256_before == .positive_preflight.workspace_payload_manifest_sha256_after and
  .positive_preflight.authority_store_created == false and
  .positive_preflight.workspace_changed == false and
  .negative_preflight.schema == "duo.launcher-preflight/v1" and
  .negative_preflight.status == "not_ready" and
  .negative_preflight.authority_scope == "unavailable" and
  .negative_preflight.failed_check.id == "host_reachability" and
  .negative_preflight.failed_check.stage == "host_reachability" and
  .negative_preflight.failed_check.status == "fail" and
  .negative_preflight.failed_check.code == "host.unreachable" and
  (.negative_preflight.failed_check.action | contains("$RUN/xdg/runtime/no-server/herdr.sock")) and
  .negative_preflight.compatibility_check.status == "not_checked" and
  .negative_preflight.compatibility_check.code == "prerequisite.not_reached" and
  .negative_preflight.duo_authority_entries_before == 0 and
  .negative_preflight.duo_authority_entries_after == 0 and
  .negative_preflight.workspace_payload_manifest_sha256_before == .negative_preflight.workspace_payload_manifest_sha256_after and
  .negative_preflight.launch_command_invoked == false and
  .negative_preflight.authority_store_created == false and
  .negative_preflight.workspace_changed == false and
  .host_state.fixture_objects_before == .host_state.fixture_objects_after and
  .host_state.fixture_objects_after == {"workspaces":0,"tabs":0,"panes":0,"agents":0} and
  .host_state.launch_observed == false and
  .herdr.compatibility == "unverified" and
  .herdr.required_full_suite_pin_available == false and
  .cleanup.server_stopped_via_local_control == true and
  .cleanup.server_process_exited == true and
  .cleanup.socket_removed == true and
  .cleanup.run_root_removed == true and
  .scrub.status == "pass"
' --arg __run '$RUN' "$summary" >/dev/null

if grep -En '/tmp/ds09|/home/dev|Bearer[[:space:]]|user_code|"access_token"|"refresh_token"|api[_-]?key|client[_-]?secret' "$summary" "$root/report.md"; then
  echo "step-09 evidence scrub check failed" >&2
  exit 1
fi

printf '%s\n' "step-09 evidence validation: pass"
