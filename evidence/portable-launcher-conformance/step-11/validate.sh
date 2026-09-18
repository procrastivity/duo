#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
summary="$root/probe-summary.json"

jq -e '
  .schema == "duo.portable-launcher-step11-evidence/v1" and
  .observed_at_utc.start[0:10] == "2026-09-18" and
  .observed_at_utc.finish[0:10] == "2026-09-18" and
  .checkout.branch == "feat/portable-launcher-conformance" and
  .checkout.commit == .duo.commit and

  # A passing Step 11 must use the accepted native executable. The npm
  # JavaScript entrypoint is recorded separately and can never satisfy it.
  .scope_result.step11 == "pass" and
  .scope_result.seal_condition_met == true and
  .scope_result.exact_accepted_native_launcher_used == true and
  .codex.version == "0.154.0" and
  .codex.accepted_version == "0.154.0" and
  .codex.native_package_version == "0.154.0-linux-x64" and
  .codex.native_executable_sha256 == "sha256:3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022" and
  .codex.accepted_native_executable_sha256 == .codex.native_executable_sha256 and
  .codex.javascript_entrypoint_sha256 == "sha256:61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70" and
  .codex.javascript_entrypoint_sha256 != .codex.native_executable_sha256 and
  .codex.diagnostic_executable == "$CODEX_NATIVE" and
  .codex.matches_accepted_pin == true and
  .codex.matches_manifest_tested_version == true and

  .installation.schema == "duo.external/v1" and
  .installation.operation == "projection.install" and
  .installation.target == "portable_launchers" and
  .installation.state == "current" and
  .skill.name == .codex_discovery.name and
  .skill.format_version == "duo.skill/duo-delegation-loop/v1" and
  .skill.content_digest == "sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182" and
  .skill.installed_file == "$RUN/workspace/.agents/skills/duo-delegation-loop/SKILL.md" and
  .skill.stamp_schema == "duo.projection-stamp/v1" and
  .skill.stamp_target == "portable_launchers" and
  .skill.installed_bytes_match_canonical == true and

  .codex_discovery.exit_code == 0 and
  .codex_discovery.matching_skill_records == 1 and
  .codex_discovery.location == .skill.installed_file and
  .codex_discovery.description_sha256 == "sha256:47c044a8e999289d69dd7912f007af0d7f11b6e10a8f5ffe28e730c7139cb85e" and
  .codex_discovery.canonical_file_digest_matches == true and
  .codex_discovery.loader_invoked == true and
  .codex_discovery.model_visible_entry_injected == true and
  .codex_discovery.model_turn_invoked == false and
  .codex_discovery.behavioral_model_adherence_claimed == false and
  .codex_discovery.threads_created == 0 and
  .codex_discovery.sessions_created == 0 and
  .codex_discovery.credentials_used == false and
  .isolation.clean_environment_used == true and
  .isolation.real_user_skill_tree_written == false and
  .isolation.account_credentials_used == false and
  (.isolation.installed_plugins | length) == 0 and
  (.isolation.configured_mcp_servers | length) == 0 and
  .isolation.plugins_enabled == false and
  .isolation.mcp_enabled == false and
  .isolation.terminal_input_used == false and

  .positive_preflight.schema == "duo.launcher-preflight/v1" and
  .positive_preflight.status == "ready" and
  .positive_preflight.authority_scope == "local" and
  .positive_preflight.host_source == "explicit-flag" and
  (.positive_preflight.checks | length) == 8 and
  [.positive_preflight.checks[].id] == [
    "duo_executable", "effective_config", "authority_store", "workspace",
    "host_selection", "host_reachability", "skill_projection", "host_compatibility"
  ] and
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
  .herdr.version == "0.9.0" and
  .herdr.protocol_identity == "herdr-socket-api/22" and
  .herdr.compatibility == "unverified" and
  .herdr.required_full_suite_pin_available == false and
  .scope_result.full_codex_suite_claimed == false and
  .scope_result.launcher_support_claimed == false and
  .cleanup.server_stopped_via_local_control == true and
  .cleanup.server_process_exited == true and
  .cleanup.socket_removed == true and
  .cleanup.run_root_removed == true and
  .scrub.status == "pass" and
  .scrub.credentials_present == false and
  .scrub.private_transcripts_present == false and
  .scrub.raw_vendor_state_present == false and
  .scrub.unsanitized_environment_values_present == false
' "$summary" >/dev/null

if grep -En '/tmp/|/home/|Bearer[[:space:]]|user_code|"access_token"|"refresh_token"|api[_-]?key|client[_-]?secret' "$summary" "$root/report.md"; then
  echo "step-11 evidence scrub check failed" >&2
  exit 1
fi

printf '%s\n' "step-11 evidence validation: pass"
