/**
 * The server authorizes a manual health check with the read capability and
 * records the administrator in its audit trail. The console must use the same
 * boundary so read-only operators can run an attributable probe.
 */
export function canRunFeatureHealthCheck(
  canRead: boolean,
  detailReady: boolean,
  hasSelection: boolean,
): boolean {
  return canRead && detailReady && hasSelection
}
