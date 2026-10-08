export type AgeTier = 'age_3_4' | 'age_5_6' | 'age_7_8'

export interface ParentAccountOption {
  parentAccountId: string
  displayName: string
  phone: string
}

export interface DisabledPeriod {
  startTime: string
  endTime: string
}

export interface ParentPolicy {
  policyId: string
  familyId: string
  childId: string
  policyVersion: number
  dailyLimitMinutes: number
  allowedCategories: string[]
  disabledPeriods: DisabledPeriod[]
  maxVolumePercent: number
  updatedAt: string
}

export interface ChildProfile {
  childId: string
  familyId: string
  nickname: string
  ageTier: AgeTier
  interests: string[]
  contentCategories: string[]
  guardianConsent: boolean
  createdAt: string
  updatedAt: string
  policyState: ChildPolicyState
}

export type ChildPolicyState =
  | { status: 'available'; policy: ParentPolicy }
  | { status: 'not_set' }
  | { status: 'unavailable'; message: string }

export function parseParentAccountOptions(payload: unknown): ParentAccountOption[] {
  const record = recordValue(payload)
  const values = arrayValue(record.accounts ?? record.families ?? record.parent_accounts)

  return values
    .map(parseParentAccountOption)
    .filter((option): option is ParentAccountOption => option !== null)
}

export function parseChildProfiles(payload: unknown): ChildProfile[] {
  const record = recordValue(payload)
  return arrayValue(record.children)
    .map(parseChildProfile)
    .filter((child): child is ChildProfile => child !== null)
}

export function parseParentPolicy(payload: unknown): ParentPolicy | null {
  const record = recordValue(payload)
  const policyId = stringValue(record.policy_id)
  if (!policyId) {
    return null
  }

  return {
    policyId,
    familyId: stringValue(record.family_id),
    childId: stringValue(record.child_id),
    policyVersion: numberValue(record.policy_version, 1),
    dailyLimitMinutes: numberValue(record.daily_limit_minutes),
    allowedCategories: stringList(record.allowed_categories),
    disabledPeriods: parseDisabledPeriods(record.disabled_periods),
    maxVolumePercent: numberValue(record.max_volume_percent),
    updatedAt: stringValue(record.updated_at),
  }
}

function parseParentAccountOption(value: unknown): ParentAccountOption | null {
  if (!isRecord(value)) {
    return null
  }
  const parentAccountId = stringValue(value.parent_account_id ?? value.id)
  if (!parentAccountId) {
    return null
  }
  return {
    parentAccountId,
    displayName: stringValue(value.display_name ?? value.parent_display_name, '未设置称呼'),
    phone: stringValue(value.phone ?? value.phone_number, '未设置手机号'),
  }
}

function parseChildProfile(value: unknown): ChildProfile | null {
  if (!isRecord(value)) {
    return null
  }
  const childId = stringValue(value.child_id)
  if (!childId) {
    return null
  }
  const ageTier = stringValue(value.age_tier) as AgeTier
  if (!isAgeTier(ageTier)) {
    return null
  }

  return {
    childId,
    familyId: stringValue(value.family_id),
    nickname: stringValue(value.nickname, '未设置称呼'),
    ageTier,
    interests: stringList(value.interests),
    contentCategories: stringList(value.content_categories),
    guardianConsent: value.guardian_consent === true,
    createdAt: stringValue(value.created_at),
    updatedAt: stringValue(value.updated_at),
    policyState: resolveChildPolicyState(value),
  }
}

/// Resolves one child's policy into a state the page can render without
/// mistaking a failed read for a missing policy.
function resolveChildPolicyState(
  value: Record<string, unknown>,
): ChildPolicyState {
  const explicitState = stringValue(value.policy_state ?? value.policy_status)
  if (explicitState === 'read_failed' || explicitState === 'unavailable') {
    return {
      status: 'unavailable',
      message: stringValue(
        value.policy_error,
        '策略暂时无法读取，请稍后重试',
      ),
    }
  }
  if (explicitState === 'not_set') {
    return { status: 'not_set' }
  }
  if (isRecord(value.policy_error)) {
    const message = stringValue(value.policy_error.message)
    return {
      status: 'unavailable',
      message: message || '策略暂时无法读取，请稍后重试',
    }
  }
  if (typeof value.policy_error === 'string' && value.policy_error.length > 0) {
    return {
      status: 'unavailable',
      message: value.policy_error,
    }
  }
  // A present null is an explicit backend statement that no policy exists.
  if (value.policy === null) {
    return { status: 'not_set' }
  }
  const policy = parseParentPolicy(value.policy)
  if (policy !== null) {
    return { status: 'available', policy }
  }
  // A missing field or a policy object that cannot be parsed means the
  // response is incomplete. Support staff must not see this as "not set".
  return {
    status: 'unavailable',
    message: '策略暂时无法读取，请稍后重试',
  }
}

function parseDisabledPeriods(value: unknown): DisabledPeriod[] {
  return arrayValue(value)
    .map((item): DisabledPeriod | null => {
      if (!isRecord(item)) {
        return null
      }
      const startTime = stringValue(item.start_time ?? item.startTime)
      const endTime = stringValue(item.end_time ?? item.endTime)
      if (!startTime || !endTime) {
        return null
      }
      return { startTime, endTime }
    })
    .filter((period): period is DisabledPeriod => period !== null)
}

function isAgeTier(value: string): value is AgeTier {
  return value === 'age_3_4' || value === 'age_5_6' || value === 'age_7_8'
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function stringList(value: unknown): string[] {
  return arrayValue(value)
    .map((item) => stringValue(item))
    .filter((item) => item.length > 0)
}

function stringValue(value: unknown, fallback = ''): string {
  return typeof value === 'string' && value.length > 0 ? value : fallback
}

function numberValue(value: unknown, fallback = 0): number {
  if (value === null || value === undefined || value === '') {
    return fallback
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function recordValue(value: unknown): Record<string, unknown> {
  return isRecord(value) ? value : {}
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
