/// Stable conflict codes understood by the feature-center draft-preserving
/// recovery flow. Runtime-backed settings and the legacy feature overlay use
/// different version domains, but both require the operator to reload before
/// retrying.
export const FEATURE_CENTER_VERSION_CONFLICT_CODES = [
  'config_version_conflict',
  'feature_config_version_conflict',
  'feature_version_conflict',
  'version_conflict',
  'stale_feature_config',
  'settings_version_conflict',
] as const
