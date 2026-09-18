#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
summary="$root/prerequisite-summary.json"

jq -e '
  . as $evidence |
  .schema == "duo.portable-launcher-step12-prerequisite-evidence/v1" and
  .observed_at_utc.start[0:10] == "2026-09-18" and
  .observed_at_utc.finish[0:10] == "2026-09-18" and
  .checkout.branch == "feat/portable-launcher-conformance" and
  (.checkout.commit | test("^[0-9a-f]{40}$")) and

  # Bind the result to the accepted native Codex ELF. The JavaScript wrapper
  # is separately identified and cannot satisfy the launcher prerequisite.
  .launcher.name == "codex" and
  .launcher.required.cli_version == "0.154.0" and
  .launcher.required.native_package_version == "0.154.0-linux-x64" and
  .launcher.required.native_executable_sha256 == "sha256:3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022" and
  .launcher.observed.cli_version == .launcher.required.cli_version and
  .launcher.observed.native_package_version == .launcher.required.native_package_version and
  .launcher.observed.native_executable_sha256 == .launcher.required.native_executable_sha256 and
  .launcher.observed.native_executable_kind == "ELF 64-bit x86-64 static-pie" and
  .launcher.observed.javascript_entrypoint_sha256 == "sha256:61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70" and
  .launcher.observed.javascript_entrypoint_sha256 != .launcher.observed.native_executable_sha256 and
  .launcher.accepted_identity_is_native_not_wrapper == true and
  .launcher.matches_exact_accepted_pin == true and

  .duo.source_commit == .checkout.commit and
  .duo.build.commit == .checkout.commit and
  .duo.build.version == "4c47e63-dirty" and
  .duo.build.executable_sha256 == "sha256:c89484c7ef0bbd1c7c1b282961e8ca7686ac7064a1ea052cf99c834753affa82" and
  .duo.build.identity_verified_by_version_command == true and
  .duo.build.artifact_retained == false and
  .duo.manifest.schema == "duo.manifest/v1" and
  .duo.manifest.portable_target == "portable_launchers" and
  .duo.manifest.portable_target_status == "unverified" and

  .skill.name == "duo-delegation-loop" and
  .skill.format_version == "duo.skill/duo-delegation-loop/v1" and
  .skill.content_digest == "sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182" and
  .skill.matches_manifest == true and

  .suite.schema == "duo.portable-launcher-conformance-scenario/v1" and
  .suite.name == "portable-launcher-delegation" and
  .suite.revision == 1 and
  .suite.manifest_digest == "sha256:01e8e6bd80ceb441be6dc128aca023a1bede31b655e74b12ee3da0c2fc6cc204" and
  .suite.oracle_digest == "sha256:a305744fc34af96b5bbd916b9de3aa1bc94f59ce4ea9316b62bb0a8dd5dd22c5" and
  .suite.task_digest == "sha256:5949809812a3ada98c95e77e9942af848e3080e4984df43a2d96fe5a24d5eab2" and
  .suite.source_and_fixture_sync_tests_passed == true and

  .runtime.required.version == "0.83.0" and
  .runtime.required.semantic_format == "pi-session-jsonl/v3" and
  .runtime.required.adapter_build == "stage1" and
  .runtime.required.conformance_record == "pi-0.83.0-2026-08-23" and
  .runtime.required.delivery_asset_sha256 == "sha256:2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23" and
  .runtime.installed.version == "0.84.4" and
  .runtime.installed.version != .runtime.required.version and
  .runtime.installed.matches_required_version == false and
  .runtime.local_cache.exact_package_archive_present == true and
  .runtime.local_cache.package_version == .runtime.required.version and
  .runtime.local_cache.package_archive_sha256 == "sha256:7097fe4b38762dda7ec78001e7b90430c849fbaf717325bfe8109744e32255e6" and
  .runtime.local_cache.dependency_tree_provisioned == false and
  .runtime.local_cache.runnable_exact_executable_available == false and
  .runtime.exact_required_runtime_available == false and

  .host_adapter.required.version == "0.8.2" and
  .host_adapter.required.protocol == "herdr-socket-api/20" and
  .host_adapter.required.api_schema_sha256 == "sha256:c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150" and
  .host_adapter.required.adapter_build == "stage1" and
  .host_adapter.required.conformance_record == "notes/19-herdr-probes.md@2026-08-23" and
  .host_adapter.installed_on_path.version == "0.9.0" and
  .host_adapter.installed_on_path.matches_required_identity == false and
  (.host_adapter.local_exact_version_candidates | length) == 2 and
  ([.host_adapter.local_exact_version_candidates[] |
    select(.version == "0.8.2" and
           .api_schema_sha256 == "sha256:24e62c2d8be455ba7872c0e66bfbbf77e8403c29c3f5b1ff10c8ff3c112f4e63" and
           .matches_required_schema == false)] | length) == 2 and
  ([.host_adapter.local_exact_version_candidates[] |
    select(.api_schema_sha256 == $evidence.host_adapter.required.api_schema_sha256)] | length) == 0 and
  .host_adapter.protocol_probe_performed == false and
  .host_adapter.exact_required_host_available == false and

  .canonical_blocked_prerequisite.name == "supported_admitted_then_blocked_producer" and
  .canonical_blocked_prerequisite.scenario_status == "unavailable" and
  .canonical_blocked_prerequisite.validator_rejects_overall_pass == true and
  .canonical_blocked_prerequisite.validator_rejects_blocked_case_pass == true and
  .canonical_blocked_prerequisite.available == false and

  ([.prerequisites[] | select(.required == true and .status == "unavailable")] | length) >= 3 and
  ([.prerequisites[] | select(.name == "pinned_codex_native_binary" and .status == "available")] | length) == 1 and
  ([.prerequisites[] | select(.name == "pinned_pi_0_83_0_runnable_artifact" and .status == "unavailable")] | length) == 1 and
  ([.prerequisites[] | select(.name == "pinned_herdr_0_8_2_protocol_20_schema" and .status == "unavailable")] | length) == 1 and
  ([.prerequisites[] | select(.name == "supported_admitted_then_blocked_producer" and .status == "unavailable")] | length) == 1 and
  .eligibility.all_exact_prerequisites_available == false and
  .eligibility.result == "blocked" and
  .eligibility.complete_suite_can_pass_current_revision == false and
  .eligibility.step12_seal_condition_met == false and
  .eligibility.matter_seal_condition_met == false and

  .actions.fixture_setup_invoked == false and
  .actions.fixture_root_created == false and
  .actions.fixture_workspace_created_or_mutated == false and
  .actions.fixture_authority_created_or_mutated == false and
  .actions.herdr_server_started == false and
  .actions.launcher_session_invoked == false and
  .actions.launcher_thread_created == false and
  .actions.model_turn_invoked == false and
  .actions.duo_session_launch_invoked == false and
  .actions.account_billed_action_invoked == false and
  .actions.credentials_inspected == false and
  .actions.production_or_shared_runtime_state_accessed == false and
  .actions.binary_downloaded == false and
  .actions.external_network_action_invoked == false and
  .actions.wip_lifecycle_state_changed == false and
  .scrub.status == "pass"
' "$summary" >/dev/null

# The evidence must remain path- and secret-scrubbed. Stable tokens such as
# $HOME, $CHECKOUT, $EVIDENCE, and $HERDR_CHECKOUT are intentional.
if grep -En '/home/dev|/tmp/|Bearer[[:space:]]|user_code|"access_token"|"refresh_token"|api[_-]?key|client[_-]?secret' "$summary" "$root/report.md"; then
  echo "step-12 evidence scrub check failed" >&2
  exit 1
fi

printf '%s\n' "step-12 prerequisite evidence validation: blocked (pass)"
