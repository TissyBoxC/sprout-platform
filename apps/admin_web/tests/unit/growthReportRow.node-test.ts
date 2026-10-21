import assert from 'node:assert/strict'
import test from 'node:test'

import { toGrowthReport } from '../../src/features/family/application/growthReportRow.js'

test('reads a growth report row into the console shape', () => {
  const report = toGrowthReport({
    id: 'report-1',
    child_id: 'child-1',
    period_type: 'monthly',
    period_start: '2026-10-01',
    period_end: '2026-11-01',
    summary: '这个月练习了 8 次。',
    generated_at: '2026-11-01T00:05:00Z',
    metrics: {
      practice: { attempts: 8, passed: 6, average_score: 82.5, accuracy: 75 },
      usage: { active_seconds: 1800, conversation_count: 12 },
      counts: { growth_records: 3, memories: 5 },
      accuracy_phrase: '75%（6/8）',
    },
  })

  assert.equal(report.id, 'report-1')
  assert.equal(report.periodType, 'monthly')
  assert.equal(report.practiceAttempts, 8)
  assert.equal(report.accuracyPhrase, '75%（6/8）')
  // The panel speaks in minutes, so seconds are truncated rather than rounded up.
  assert.equal(report.activeMinutes, 30)
  assert.equal(report.growthRecords, 3)
})

test('truncates partial minutes instead of rounding them up', () => {
  const report = toGrowthReport({
    metrics: { usage: { active_seconds: 119 } },
  })

  assert.equal(report.activeMinutes, 1)
})

test('treats an unknown period as weekly rather than crashing', () => {
  const report = toGrowthReport({ period_type: 'quarterly' })

  assert.equal(report.periodType, 'weekly')
})

test('degrades a malformed row to empty fields', () => {
  const report = toGrowthReport({
    id: 42,
    period_type: null,
    metrics: 'not-an-object',
  })

  assert.equal(report.id, '')
  assert.equal(report.summary, '')
  assert.equal(report.practiceAttempts, 0)
  assert.equal(report.activeMinutes, 0)
  assert.equal(report.periodType, 'weekly')
})

test('survives a null row', () => {
  const report = toGrowthReport(null)

  assert.equal(report.id, '')
  assert.equal(report.growthRecords, 0)
})
