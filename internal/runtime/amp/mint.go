package amp

// LaunchMintPrompt is the print prompt the mint wrapper script
// (MaterializeMintScript) feeds `amp -x` on stdin so launch mints an Amp
// thread and exits; it is not a skill instruction. Mirrors
// devin.LaunchMintPrompt's role for the Devin print-mint launch.
const LaunchMintPrompt = "Reply with exactly: DUO-AMP-READY"

// MintPromptEnvVar is the environment variable the mint wrapper script
// (MaterializeMintScript) reads its prompt from, so the script body
// itself stays constant across launches (docs/cli/decisions.md,
// 2026-09-09, "Amp mint delivery needs a Duo-materialized wrapper
// script").
const MintPromptEnvVar = "DUO_AMP_MINT_PROMPT"
