export type FeatureMetricsState =
  | 'not_configured'
  | 'available'
  | 'unavailable'
  | 'not_reported'

/**
 * Normalizes the server's explicit metrics state. Older backends omitted the
 * field, so their response shape remains the compatibility fallback.
 */
export function normalizeMetricsState(
  value: unknown,
  hasMetricsField: boolean,
): FeatureMetricsState {
  const normalized = typeof value === 'string' ? value.trim().toLowerCase() : ''
  switch (normalized) {
    case 'not_configured':
    case 'available':
    case 'unavailable':
    case 'not_reported':
      return normalized
    default:
      return hasMetricsField ? 'available' : 'not_configured'
  }
}
