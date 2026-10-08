import { expect, test, type Page, type Route } from '@playwright/test'

interface MockFeatureConfigField {
  key: string
  label: string
  type:
    | 'boolean'
    | 'integer'
    | 'number'
    | 'string'
    | 'select'
    | 'multiselect'
    | 'duration'
    | 'url'
    | 'secret'
  required?: boolean
  default_value?: unknown
  options?: Array<{ value: string; label: string }>
  minimum?: number
  maximum?: number
  step?: number
  description?: string
  value?: unknown
  help?: string
  editable?: boolean
  allow_custom?: boolean
}

interface MockFeature {
  id: string
  name: string
  description: string
  owner: string
  category: string
  category_label: string
  status: string
  enabled: boolean
  health: string
  health_source: string
  last_health_check_at?: string
  health_message?: string
  health_latency_ms?: number
  config_version: number
  config_schema: MockFeatureConfigField[]
  values: Record<string, unknown>
  read_only_metrics: Array<{
    key: string
    label: string
    value: string | null
    unit?: string
  }>
  read_only_reason?: string
  updated_at: string
}

async function useAdminSession(page: Page): Promise<void> {
  await page.addInitScript(() => {
    globalThis.sessionStorage.setItem('sprout.admin.accessToken', 'test-access-token')
    globalThis.sessionStorage.setItem('sprout.admin.refreshToken', 'test-refresh-token')
  })
  await page.route('**/api/v1/auth/me', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          account: {
            id: 'test-admin',
            email: 'admin@sprout.local',
            display_name: '管理员',
            role: 'admin',
          },
        },
      }),
    })
  })
}

function jsonResponse(data: unknown, status = 200): {
  status: number
  contentType: string
  body: string
} {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify({
      schema_version: '1.0.0',
      request_id: 'feature-center-e2e',
      data,
      error: null,
    }),
  }
}

function conflictResponse(): {
  status: number
  contentType: string
  body: string
} {
  return {
    status: 409,
    contentType: 'application/json',
    body: JSON.stringify({
      schema_version: '1.0.0',
      request_id: 'feature-center-conflict',
      data: null,
      error: {
        code: 'feature_config_version_conflict',
        message: '功能配置版本已变化',
        retryable: true,
      },
    }),
  }
}

function mockFeature(): MockFeature {
  return {
    id: 'voice_conversation',
    name: '语音对话',
    description: '为孩子提供安全、适龄的语音陪伴。',
    owner: 'Voice Gateway',
    category: 'conversation',
    category_label: '对话能力',
    status: 'enabled',
    enabled: true,
    config_version: 3,
    config_schema: [
      {
        key: 'enabled',
        label: '启用语音对话',
        type: 'boolean',
        description: '关闭后孩子无法发起语音对话。',
        value: true,
      },
      {
        key: 'max_reply_seconds',
        label: '最长回复时长',
        type: 'integer',
        minimum: 1,
        maximum: 60,
        step: 1,
        value: 12,
      },
      {
        key: 'system_prompt',
        label: '系统提示词',
        type: 'string',
        value: '保持温和、简洁。',
      },
      {
        key: 'upstream_api_key',
        label: '上游服务密钥',
        type: 'secret',
        description: '仅显示是否已经配置，不会回显密钥内容。',
        value: {
          configured: true,
          mask: '****alue',
        },
      },
    ],
    values: {
      enabled: true,
      max_reply_seconds: 12,
      system_prompt: '保持温和、简洁。',
    },
    read_only_metrics: [
      {
        key: 'conversations_today',
        label: '今日对话',
        value: '128',
        unit: '次',
      },
      {
        key: 'average_latency_ms',
        label: '平均响应',
        value: '340',
        unit: '毫秒',
      },
    ],
    health: 'healthy',
    health_source: 'service_runtime',
    last_health_check_at: '2026-10-08T09:30:00Z',
    health_message: '最近 5 分钟运行正常。',
    health_latency_ms: 82,
    updated_at: '2026-10-08T09:00:00Z',
  }
}

async function setupFeatureCenterRoutes(
  page: Page,
  options: {
    feature?: Partial<MockFeature>
    onUpdate?: (route: Route, feature: MockFeature) => Promise<void>
  } = {},
): Promise<{
  feature: MockFeature
  updatePayloads: Array<Record<string, unknown>>
  healthChecks: number[]
}> {
  const feature: MockFeature = {
    ...mockFeature(),
    ...options.feature,
  }
  const updatePayloads: Array<Record<string, unknown>> = []
  const healthChecks: number[] = []

  await page.route(/\/api\/v1\/admin\/feature-center$/, async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    await route.fulfill(jsonResponse({ features: [feature] }))
  })

  await page.route(
    /\/api\/v1\/admin\/feature-center\/voice_conversation$/,
    async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill(jsonResponse({ feature }))
        return
      }
      await route.continue()
    },
  )

  await page.route(
    /\/api\/v1\/admin\/feature-center\/voice_conversation\/config$/,
    async (route) => {
      const payload = route.request().postDataJSON() as Record<string, unknown>
      updatePayloads.push(payload)
      if (options.onUpdate !== undefined) {
        await options.onUpdate(route, feature)
        return
      }
      const values = payload.values as Record<string, unknown>
      feature.values = { ...feature.values, ...values }
      feature.config_version += 1
      await route.fulfill(jsonResponse({ feature }))
    },
  )

  await page.route(
    /\/api\/v1\/admin\/feature-center\/voice_conversation\/health-check$/,
    async (route) => {
      healthChecks.push(Date.now())
      feature.health = 'degraded'
      feature.last_health_check_at = '2026-10-08T10:00:00Z'
      feature.health_message = '上游响应略有波动。'
      feature.health_latency_ms = 260
      await route.fulfill(
        jsonResponse({
          feature,
        }),
      )
    },
  )

  return { feature, updatePayloads, healthChecks }
}

test('loads, filters, edits, and saves feature configuration without returning secrets', async ({
  page,
}) => {
  await useAdminSession(page)
  const { updatePayloads } = await setupFeatureCenterRoutes(page)

  await page.goto('/feature-center')

  await expect(page.getByRole('heading', { name: '功能中心' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '语音对话' })).toBeVisible()
  await expect(page.getByText('今日对话')).toBeVisible()
  await expect(page.getByText('128', { exact: true })).toBeVisible()

  const secretInput = page.getByLabel('上游服务密钥')
  await expect(secretInput).toHaveValue('')
  await expect(secretInput).toHaveAttribute('type', 'password')
  await expect(page.getByText('当前状态：已配置')).toBeVisible()
  await expect(page.getByText('real-secret-value')).toHaveCount(0)
  await expect(page.getByText('****alue')).toHaveCount(0)

  await page.getByLabel('搜索功能').fill('语音')
  await expect(page.getByRole('button', { name: /语音对话/ })).toBeVisible()
  await page.getByLabel('按类别筛选').selectOption('conversation')
  await expect(page.getByText('1 / 1 个功能')).toBeVisible()

  await page.getByLabel('启用语音对话').uncheck()
  await page.getByLabel('最长回复时长').fill('24')
  await page.getByRole('button', { name: '保存配置' }).click()

  await expect(page.getByText('语音对话 的配置已保存。')).toBeVisible()
  expect(updatePayloads).toHaveLength(1)
  expect(updatePayloads[0]).toEqual({
    values: {
      enabled: false,
      max_reply_seconds: 24,
      system_prompt: '保持温和、简洁。',
    },
    expected_version: 3,
  })
  expect(JSON.stringify(updatePayloads[0])).not.toContain('real-secret-value')
})

test('shows a merge-and-reload message when the feature configuration version conflicts', async ({
  page,
}) => {
  await useAdminSession(page)
  await setupFeatureCenterRoutes(page, {
    onUpdate: async (route) => {
      await route.fulfill(conflictResponse())
    },
  })

  await page.goto('/feature-center')
  await page.getByLabel('启用语音对话').uncheck()
  await page.getByRole('button', { name: '保存配置' }).click()

  await expect(
    page.getByText(/这项功能已被其他人更新/),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByText('语音对话 的配置已保存。')).toHaveCount(0)
})

test('runs a feature health check and updates the visible health result', async ({
  page,
}) => {
  await useAdminSession(page)
  const { healthChecks } = await setupFeatureCenterRoutes(page)

  await page.goto('/feature-center')

  await expect(page.getByText('健康状态：运行正常')).toBeVisible()
  await expect(page.getByText(/最后检查：2026\/10\/8/)).toBeVisible()

  await page.getByRole('button', { name: '检查健康' }).click()

  await expect(page.getByText('健康状态：存在波动')).toBeVisible()
  await expect(page.getByText('上游响应略有波动。')).toBeVisible()
  await expect(page.getByText(/最后检查：2026\/10\/8/)).toBeVisible()
  expect(healthChecks).toHaveLength(1)
})

test('keeps a planned feature read-only and explains why configuration is unavailable', async ({
  page,
}) => {
  await useAdminSession(page)
  await setupFeatureCenterRoutes(page, {
    feature: {
      status: 'planned',
      enabled: false,
      read_only_reason: '该功能尚未实现，当前仅展示规划信息。',
    },
  })

  await page.goto('/feature-center')

  await expect(page.getByText('规划中', { exact: true }).first()).toBeVisible()
  await expect(
    page.getByText('该功能尚未实现，当前仅展示规划信息。'),
  ).toBeVisible()
  await expect(page.getByLabel('启用语音对话')).toBeDisabled()
  await expect(page.getByRole('button', { name: '保存配置' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '放弃修改' })).toBeDisabled()
})

test('shows an explicit unavailable-health state when no health probe is connected', async ({
  page,
}) => {
  await useAdminSession(page)
  await setupFeatureCenterRoutes(page, {
    feature: {
      health: 'unknown',
      health_source: 'not_configured',
      last_health_check_at: undefined,
      health_message: undefined,
      health_latency_ms: undefined,
      read_only_metrics: [
        {
          key: 'conversations_today',
          label: '今日对话',
          value: null,
          unit: '次',
        },
      ],
    },
  })

  await page.goto('/feature-center')

  await expect(
    page.getByText('健康状态：暂未接入健康检查'),
  ).toBeVisible()
  await expect(
    page.getByText('这项功能还没有接入可用的健康检查。'),
  ).toBeVisible()
  await expect(page.getByText('最后检查：暂未接入')).toBeVisible()
  await expect(page.getByText('暂未接入', { exact: true })).toBeVisible()
})

test('adds, preserves, removes, and saves a custom multiselect value when allowed', async ({
  page,
}) => {
  await useAdminSession(page)
  const { updatePayloads } = await setupFeatureCenterRoutes(page, {
    feature: {
      config_schema: [
        {
          key: 'model_ids',
          label: '模型 ID',
          type: 'multiselect',
          allow_custom: true,
          options: [
            { value: 'model-a', label: '模型 A' },
            { value: 'model-b', label: '模型 B' },
          ],
          value: ['legacy-model'],
        },
      ],
      values: {
        model_ids: ['legacy-model'],
      },
    },
  })

  await page.goto('/feature-center')

  await expect(page.getByText('legacy-model', { exact: true })).toBeVisible()
  await page.getByLabel('模型 A').check()
  await expect(page.getByText('legacy-model', { exact: true })).toBeVisible()

  const customInput = page.getByLabel('模型 ID')
  await customInput.fill('model-nightly')
  await page.getByRole('button', { name: '添加' }).click()

  await expect(page.getByText('model-nightly', { exact: true })).toBeVisible()
  await expect(page.getByText('legacy-model', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '移除 model-nightly' }).click()
  await expect(page.getByText('model-nightly', { exact: true })).toHaveCount(0)

  await customInput.fill('model-nightly')
  await page.getByRole('button', { name: '添加' }).click()
  await page.getByRole('button', { name: '保存配置' }).click()

  await expect(page.getByText('语音对话 的配置已保存。')).toBeVisible()
  expect(updatePayloads).toHaveLength(1)
  expect(updatePayloads[0]).toEqual({
    values: {
      model_ids: ['legacy-model', 'model-a', 'model-nightly'],
    },
    expected_version: 3,
  })
})
