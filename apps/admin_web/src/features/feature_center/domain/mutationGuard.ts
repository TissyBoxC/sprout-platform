/**
 * Prevents a late feature-center response from replacing a newer selection.
 * The selected id is checked as well as the request generation so the guard
 * remains correct even if two selections share the same helper call path.
 */
export function canApplyMutationResult(
  requestGeneration: number,
  currentGeneration: number,
  requestedFeatureId: string,
  selectedFeatureId: string,
): boolean {
  return (
    requestGeneration === currentGeneration &&
    requestedFeatureId !== '' &&
    requestedFeatureId === selectedFeatureId
  )
}
