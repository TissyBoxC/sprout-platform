# UI Text Feature

Owns the admin workflow for drafts, preview, approval, publication, version
history, rollback, audit, and per-platform constraints.

Security-sensitive text requires a separate reviewer. Ordinary text can be
published by an authorized content editor.

The page lists every stored version across package id, language, and platform,
opens one version's entries, and drives the lifecycle
(`draft -> in_review -> published -> withdrawn`). Publication requires the
detached Ed25519 signature and key id that the release tooling produces, and a
withdrawal advances the same catalog revision clients poll, so removals reach
devices without a separate refresh path.
