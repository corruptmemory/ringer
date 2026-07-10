// Package ringer embeds the curated reference assets that ship inside the
// static binary: the model-identity registry and the MODEL-NOTES judgment
// layer. They live at the repo root (registry/, docs/) — frozen locations
// kept past cutover — and //go:embed cannot reach upward from a nested
// package, so the embed directives live here at the module root. Config
// keys ([scoreboard] model_identity_path / model_notes_path) override these
// defaults with a live on-disk file.
package ringer

import _ "embed"

//go:embed registry/model-identity.toml
var ModelIdentityTOML []byte

//go:embed docs/MODEL-NOTES.md
var ModelNotesMD []byte
