/// Shape of one generated growth report as the operator sees it.
///
/// Only aggregates and a platform-written summary reach the console: a report
/// never carries conversation text, a memory value or any media.
export interface AdminGrowthReport {
  id: string
  childId: string
  periodType: 'weekly' | 'monthly'
  periodStart: string
  periodEnd: string
  summary: string
  accuracyPhrase: string
  practiceAttempts: number
  activeMinutes: number
  growthRecords: number
  generatedAt: string
}

/// Reads one growth report row from the platform response.
///
/// A malformed row degrades to empty fields instead of throwing, so a single
/// unexpected report cannot blank the whole panel and hide the other periods.
export function toGrowthReport(value: unknown): AdminGrowthReport {
  const record = toRecord(value)
  const metrics = toRecord(record.metrics)
  const practice = toRecord(metrics.practice)
  const usage = toRecord(metrics.usage)
  const counts = toRecord(metrics.counts)
  const periodType = toStringValue(record.period_type)
  return {
    id: toStringValue(record.id),
    childId: toStringValue(record.child_id),
    periodType: periodType === 'monthly' ? 'monthly' : 'weekly',
    periodStart: toStringValue(record.period_start),
    periodEnd: toStringValue(record.period_end),
    summary: toStringValue(record.summary),
    accuracyPhrase: toStringValue(metrics.accuracy_phrase),
    practiceAttempts: toNumberValue(practice.attempts),
    // Seconds are only useful for debugging; the panel speaks in minutes.
    activeMinutes: Math.floor(toNumberValue(usage.active_seconds) / 60),
    growthRecords: toNumberValue(counts.growth_records),
    generatedAt: toStringValue(record.generated_at),
  }
}

function toRecord(value: unknown): Record<string, unknown> {
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function toStringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function toNumberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value
  }
  return 0
}
