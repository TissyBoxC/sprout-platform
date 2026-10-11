# UI Text Module

Owns draft review, approval, publication, rollback, and signed delivery of
remote UI text packages.

The module is split into domain, service, repository, and handler layers.
Draft and entry validation live in the domain package so every caller shares
one rule set. It does not own learning content, AI prompts, model credentials,
or child-specific copy.
