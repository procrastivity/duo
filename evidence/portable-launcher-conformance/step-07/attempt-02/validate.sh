#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
summary="$root/probe-summary.json"
report="$root/report.md"

jq -e '
  .schema == "duo.portable-launcher-step07-evidence/v2" and
  .attempt == 2 and
  .observed_at_utc.start == "2026-09-18T10:20:59Z" and
  .observed_at_utc.finish == "2026-09-18T10:28:17Z" and
  .host.kernel == "Linux 6.8.0-136-generic" and
  .host.architecture == "x86_64" and
  .checkout.path == "$CHECKOUT" and
  .checkout.branch == "feat/portable-launcher-rolling-evidence" and
  .checkout.commit == "61bb889f1d70e4a1654a53e0828be52638081a02" and
  .checkout.describe == "61bb889-dirty" and
  .checkout.preexisting_out_of_scope_changes == true and
  .checkout.commit == .checkout.policy_commit and
  .checkout.commit == .duo.commit and

  .amp.name == "amp" and
  .amp.version == "0.0.1789724374-g0d2ed0" and
  .amp.release_time == "2026-09-18T09:39:34.000Z" and
  .amp.command_path == "$AMP_COMMAND" and
  .amp.source_path == "$AMP_SOURCE" and
  .amp.copied_executable_path == "$RUN/bin/amp" and
  .amp.executable_bytes == 100795872 and
  .amp.executable_sha256 == "sha256:7c45e90d54d8a46adf8069077341e85471c6bff3e2a881afc00f0ab35b188f91" and
  .amp.copy_mode == "0700" and
  (.amp.copy_attempts >= 1 and .amp.copy_attempts <= 2) and
  .amp.maximum_copy_attempts == 2 and
  .amp.copy_retry_policy == "On any source stat or SHA-256 change during copy, discard the copy and retry once from a fresh source observation; fail after a second change." and
  .amp.source_stable_during_copy == true and
  .amp.source_sha256_before == .amp.executable_sha256 and
  .amp.source_sha256_after == .amp.executable_sha256 and
  .amp.copied_sha256_after_placement == .amp.executable_sha256 and
  .amp.commands_used_copied_bytes == true and
  .amp.historical_tested_version == "0.0.1789675234-g2899fe" and
  .amp.present_in_historical_tested_versions == false and

  .duo.version == "61bb889-dirty" and
  .duo.build_date == "2026-09-18T10:22:15Z" and
  .duo.build_command == "CGO_ENABLED=0 go build -trimpath -ldflags <version,full-commit,date> -o $RUN/bin/duo ./cmd/duo" and
  .duo.executable_path == "$RUN/bin/duo" and
  .duo.executable_bytes == 18474494 and
  .duo.executable_sha256 == "sha256:ecdbb54dd8f38e44ea6cac0abeb855be97ecde6f9cd2a0ea9f5846cf356a6bd9" and
  .duo.manifest_schema == "duo.manifest/v1" and
  .duo.manifest_digest == "sha256:fdf2dfe00b8638976bf319fd88e4b8f03db29e680ea9cc723e06d36fe354d172" and

  .policy.launcher_eligibility == "capability_evidence" and
  .policy.launcher_recognized == true and
  .policy.historical_tested_versions_are_eligibility_gate == false and
  .policy.manifest_target == "portable_launchers" and
  .policy.manifest_status == "unverified" and
  .policy.canonical_scenario == "portable-launcher-delegation/v1" and
  .policy.canonical_scenario_revision == 2 and
  .policy.canonical_scenario_sha256 == "sha256:b08b8a4f715137e60d098e725442f925f28b8b66970e3188428927ab447820d4" and

  .installation.exit_code == 0 and
  .installation.command == "$RUN/bin/duo install portable-launchers --workspace $RUN/workspace --output json" and
  .installation.schema == "duo.external/v1" and
  .installation.operation == "projection.install" and
  .installation.target == .policy.manifest_target and
  .installation.state == "current" and
  .installation.changed == true and
  (.installation.warnings | length) == 0 and
  (.installation.features | length) == 0 and
  .skill.name == "duo-delegation-loop" and
  .skill.name == .amp_discovery.name and
  .skill.format_version == "duo.skill/duo-delegation-loop/v1" and
  .skill.content_digest == "sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182" and
  .skill.size_bytes == 6284 and
  .skill.canonical_path == "$CHECKOUT/skills/duo-delegation-loop/SKILL.md" and
  .skill.installation_id == "801f13028ed977a2764e01695764ea29" and
  .skill.installed_root == "$RUN/workspace/.agents/skills/duo-delegation-loop" and
  .skill.installed_file == "$RUN/workspace/.agents/skills/duo-delegation-loop/SKILL.md" and
  .skill.installed_stamp == "$RUN/workspace/.agents/skills/duo-delegation-loop/.duo-generated.json" and
  .skill.installed_file_mode == "0600" and
  .skill.installed_bytes_match_canonical == true and
  .skill.stamp_schema == "duo.projection-stamp/v1" and
  .skill.stamp_target == .policy.manifest_target and
  .skill.stamp_launcher_eligibility == .policy.launcher_eligibility and
  .skill.stamp_manifest_digest == .duo.manifest_digest and
  .skill.stamp_sha256 == "sha256:700c2cc3b3f5bcb61651b015e34c0c57794778a6a0a2753b00e018cb1294d8e4" and
  .skill.historical_amp_tested_versions == [.amp.historical_tested_version] and
  (.amp.version as $amp_version | (.skill.historical_amp_tested_versions | index($amp_version)) == null) and

  .amp_discovery.commands == [
    "$RUN/bin/amp skill list --json --settings-file $RUN/noauth/config/amp/settings.json",
    "$RUN/bin/amp skill info duo-delegation-loop --json --settings-file $RUN/noauth/config/amp/settings.json"
  ] and
  .amp_discovery.exit_codes == [0, 0] and
  .amp_discovery.matching_project_skill_records == 1 and
  .amp_discovery.all_project_skill_records == 1 and
  .amp_discovery.source == "workspace-agents" and
  .amp_discovery.base_dir == "file://$RUN/workspace/.agents/skills/duo-delegation-loop" and
  .amp_discovery.info_path == .amp_discovery.base_dir and
  .amp_discovery.list_description_sha256 == "sha256:47c044a8e999289d69dd7912f007af0d7f11b6e10a8f5ffe28e730c7139cb85e" and
  .amp_discovery.info_description_sha256 == .amp_discovery.list_description_sha256 and
  (.amp_discovery.skill_errors | length) == 0 and
  .amp_discovery.reported_locator_matches_installed_projection == true and
  .amp_discovery.reported_file_digest_matches_canonical == true and
  .amp_discovery.loader_invoked == true and
  .amp_discovery.model_turn_invoked == false and
  .amp_discovery.behavioral_model_adherence_claimed == false and
  .amp_discovery.threads_created == 0 and

  .isolation.run_root == "$RUN" and
  .isolation.run_root_mode == "0700" and
  .isolation.home == "$RUN/noauth/home" and
  .isolation.xdg_config_home == "$RUN/noauth/config" and
  .isolation.xdg_data_home == "$RUN/noauth/data" and
  .isolation.xdg_state_home == "$RUN/noauth/state" and
  .isolation.xdg_cache_home == "$RUN/noauth/cache" and
  .isolation.workspace == "$RUN/workspace" and
  .isolation.clean_environment_used == true and
  .isolation.amp_update_checks_disabled == true and
  .isolation.global_agent_skill_roots_disabled == true and
  .isolation.claude_compatible_skill_roots_disabled == true and
  .isolation.global_repository_auth_sentinel == "known-invalid non-secret" and
  .isolation.account_credentials_used == false and
  .isolation.real_user_skill_tree_written == false and
  .isolation.project_plugin_root_present == false and
  .isolation.system_plugin_root_present == false and
  .isolation.plugins_enabled == false and
  .isolation.mcp_servers_configured == 0 and
  .isolation.mcp_permissions_reject_all == true and
  .isolation.mcp_enabled == false and
  .isolation.terminal_input_used == false and
  .isolation.launcher_state_created_only_under_run_root == true and

  .config.schema == "duo.config/v3" and
  .config.path == "$RUN/xdg/config/duo/duo.config.yaml" and
  .config.source_bytes_sha256 == "sha256:1b4cb5fafef7a324beb46b8d8529ee76d4cc1218c41d5da24251b0872b293840" and
  .config.effective_digest == "sha256:92db1583823b8d8ce7384449896cea6c641b3b4c7df5d50ce56d43a0092826ae" and
  .config.presets == ["builder"] and
  .authority.path == "$RUN/xdg/data/duo/duo.db" and
  .authority.state == "initializable" and
  .authority.present == false and
  .authority.healthy == true and
  .authority.schema_version == 0 and
  .authority.writer_active == false and
  .herdr.version == "0.9.0" and
  .herdr.source_path == "$HERDR_SOURCE" and
  .herdr.copied_executable_path == "$RUN/bin/herdr" and
  .herdr.executable_bytes == 39728936 and
  .herdr.protocol_identity == "herdr-socket-api/22" and
  .herdr.executable_sha256 == "sha256:5ef212a3f142f902b1a4eb3f7a76827ded9f313804c580c48297c060ac1fcac0" and
  .herdr.schema_export_sha256 == "sha256:226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a" and
  .herdr.compatibility == "unverified" and
  .herdr.full_suite_fixture_claimed == false and
  .herdr.required_full_suite_pin.version == "0.8.2" and
  .herdr.required_full_suite_pin.protocol_identity == "herdr-socket-api/20" and
  .herdr.required_full_suite_pin.schema_digest == "sha256:c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150" and

  .positive_preflight.command == "$RUN/bin/duo doctor --workspace $RUN/workspace --config $RUN/xdg/config/duo/duo.config.yaml --host herdr:$RUN/xdg/runtime/herdr-step07/herdr.sock --output json" and
  .positive_preflight.exit_code == 0 and
  .positive_preflight.schema == "duo.launcher-preflight/v1" and
  .positive_preflight.status == "ready" and
  .positive_preflight.authority_scope == "local" and
  .positive_preflight.host_source == "explicit-flag" and
  [.positive_preflight.checks[].id] == [
    "duo_executable", "effective_config", "authority_store", "workspace",
    "host_selection", "host_reachability", "skill_projection", "host_compatibility"
  ] and
  ([.positive_preflight.checks[] | select(.required == true and .status == "pass" and .code == "ok")] | length) == 7 and
  ([.positive_preflight.checks[] | select(.id == "host_compatibility" and .required == false and .status == "warning" and .code == "host.compatibility_unverified")] | length) == 1 and
  .positive_preflight.skill_projection_state == "current" and
  .positive_preflight.host_reachable == true and
  .positive_preflight.host_compatibility_required_for_readiness == false and
  .positive_preflight.duo_authority_entries_before == 0 and
  .positive_preflight.duo_authority_entries_after == 0 and
  .positive_preflight.workspace_payload_manifest_sha256_before == .positive_preflight.workspace_payload_manifest_sha256_after and
  .positive_preflight.authority_store_created == false and
  .positive_preflight.workspace_changed == false and

  .negative_preflight.command == "$RUN/bin/duo doctor --workspace $RUN/workspace --config $RUN/xdg/config/duo/duo.config.yaml --host herdr:$RUN/xdg/runtime/no-server/herdr.sock --output json" and
  .negative_preflight.exit_code == 0 and
  .negative_preflight.schema == "duo.launcher-preflight/v1" and
  .negative_preflight.status == "not_ready" and
  .negative_preflight.authority_scope == "unavailable" and
  .negative_preflight.host_source == "explicit-flag" and
  .negative_preflight.failed_check.id == "host_reachability" and
  .negative_preflight.failed_check.stage == "host_reachability" and
  .negative_preflight.failed_check.required == true and
  .negative_preflight.failed_check.status == "fail" and
  .negative_preflight.failed_check.code == "host.unreachable" and
  (.negative_preflight.failed_check.action | contains("$RUN/xdg/runtime/no-server/herdr.sock")) and
  .negative_preflight.compatibility_check.status == "not_checked" and
  .negative_preflight.compatibility_check.code == "prerequisite.not_reached" and
  .negative_preflight.duo_authority_entries_before == 0 and
  .negative_preflight.duo_authority_entries_after == 0 and
  .negative_preflight.workspace_payload_manifest_sha256_before == .negative_preflight.workspace_payload_manifest_sha256_after and
  .negative_preflight.workspace_payload_manifest_sha256_before == .positive_preflight.workspace_payload_manifest_sha256_after and
  .negative_preflight.launch_command_invoked == false and
  .negative_preflight.authority_store_created == false and
  .negative_preflight.workspace_changed == false and
  .negative_preflight.host_state_changed == false and
  .host_state.fixture_objects_before == {"workspaces":0,"tabs":0,"panes":0,"agents":0} and
  .host_state.fixture_objects_after == .host_state.fixture_objects_before and
  .host_state.launch_observed == false and

  .scope_result.step07 == "pass" and
  .scope_result.seal_condition_met == true and
  .scope_result.recognized_launcher_capability_assertions_passed == true and
  .scope_result.historical_tested_version_absence_is_blocker == false and
  .scope_result.full_amp_suite_claimed == false and
  .scope_result.launcher_support_claimed == false and
  .cross_repo.classification == "invalidates_assumption" and
  .cross_repo.repository == "duo-lab" and
  .cross_repo.observation_scope == "planning-only Amp observation" and
  .cross_repo.exact_amp_version == .amp.version and
  .cross_repo.observation_date == "2026-09-18" and
  .cross_repo.effect == "Invalidates the planning assumption that a current recognized Amp absent from historical tested_versions blocks Step 07; it does not unblock the full runtime suite or establish launcher support." and
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

# Evidence content may retain only symbolic roots. Reject raw local paths,
# common secret shapes, and positive claims for every prohibited mechanism.
if grep -Eini '/(tmp|home|private|var|nix|Users)/|file:///(tmp|home|private|var|nix|Users)/|[A-Za-z]:\\|Bearer[[:space:]]|sgamp_|user_code|"access_token"|"refresh_token"|api[_-]?key|client[_-]?secret|password[[:space:]]*[:=]' "$summary" "$report"; then
  echo "step-07 attempt-02 evidence scrub check failed" >&2
  exit 1
fi

for required in \
  '0.0.1789724374-g0d2ed0' \
  '7c45e90d54d8a46adf8069077341e85471c6bff3e2a881afc00f0ab35b188f91' \
  '61bb889f1d70e4a1654a53e0828be52638081a02' \
  'ecdbb54dd8f38e44ea6cac0abeb855be97ecde6f9cd2a0ea9f5846cf356a6bd9' \
  'fdf2dfe00b8638976bf319fd88e4b8f03db29e680ea9cc723e06d36fe354d172' \
  '6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182' \
  'launcher_eligibility: capability_evidence' \
  'Classification: invalidates an assumption in `duo-lab`; it does not unblock the full runtime suite.'
do
  grep -F -- "$required" "$report" >/dev/null || {
    echo "step-07 attempt-02 report identity binding failed: $required" >&2
    exit 1
  }
done

printf '%s\n' "step-07 attempt-02 evidence validation: pass"
