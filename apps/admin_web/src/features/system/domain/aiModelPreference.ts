export interface AIModelPreference {
  id: string
  latencyMs: number | null
  isRecommended?: boolean
}

/**
 * Selects the model a new AI account should receive when the operator has not
 * saved an explicit default. The gateway recommendation is authoritative;
 * latency is only a fallback when no recommendation is available.
 */
export function preferredDefaultModel<T extends AIModelPreference>(
  models: readonly T[],
): T | null {
  const recommended = models.find((model) => model.id && model.isRecommended)
  if (recommended !== undefined) {
    return recommended
  }
  const measuredModels = models.filter(
    (model) => model.id && model.latencyMs !== null && model.latencyMs >= 0,
  )
  if (measuredModels.length === 0) {
    return models.find((model) => model.id) ?? null
  }
  return measuredModels.reduce((fastest, model) =>
    (model.latencyMs ?? Number.POSITIVE_INFINITY) <
    (fastest.latencyMs ?? Number.POSITIVE_INFINITY)
      ? model
      : fastest,
  )
}
