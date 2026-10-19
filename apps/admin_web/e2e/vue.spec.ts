import { test, expect, type Page } from '@playwright/test'

async function useAdminSession(page: Page): Promise<void> {
  await useAdminSessionWithPermissions(page, [
    'admins.read',
    'admins.manage',
    'devices.read',
    'devices.manage',
    'family.read',
    'family.manage',
    'ai_accounts.read',
    'ai_accounts.manage',
    'ota.read',
    'ota.manage',
    'storage.read',
    'storage.manage',
    'content.read',
    'content.review',
    'service.read',
    'service.upgrade',
    'feature_center.read',
    'feature_center.manage',
    'settings.read',
    'settings.manage',
    'notifications.read',
    'notifications.manage',
    'stats.read',
    'audit.read',
  ])
}

async function useAdminSessionWithPermissions(page: Page, permissions: string[]): Promise<void> {
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
            admin_permissions: permissions,
          },
        },
      }),
    })
  })
}

function featurePayload(
  id: string,
  name: string,
  category: string,
  updatedAt: string,
): Record<string, unknown> {
  return {
    id,
    name,
    description: `${name}说明`,
    owner: '测试服务',
    category,
    category_label: category === 'conversation' ? '对话能力' : '内容能力',
    status: 'enabled',
    enabled: true,
    health: 'healthy',
    health_source: 'service_runtime',
    last_health_check_at: '2026-10-18T09:30:00Z',
    health_message: '运行正常',
    health_latency_ms: 20,
    config_version: 1,
    config_schema: [],
    values: {},
    read_only_metrics: [],
    read_only_reason: '',
    updated_at: updatedAt,
  }
}

test('visits the app root url', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/overview', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { overview: {} } }),
    })
  })
  await page.goto('/')
  await expect(page.locator('h1')).toHaveText('平台状态')
  await expect(page).toHaveTitle('如此萌屋管理端')
})

test('logout clears cached user-domain stores before the next session', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/families**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-logout',
              display_name: '退出前家长',
              phone: '13800000000',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { models: [] } }),
    })
  })
  await page.route('**/api/v1/auth/logout', async (route) => {
    await route.fulfill({ contentType: 'application/json', body: '{"data":{}}' })
  })

  await page.goto('/families')
  await expect(page.getByText('退出前家长', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page).toHaveURL(/\/login$/)

  await page.evaluate(() => {
    globalThis.sessionStorage.setItem('sprout.admin.accessToken', 'next-access-token')
    globalThis.sessionStorage.setItem('sprout.admin.refreshToken', 'next-refresh-token')
  })
  let familyRequests = 0
  await page.route('**/api/v1/admin/families**', async (route) => {
    familyRequests += 1
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-next',
              display_name: '新会话家长',
              phone: '13900000000',
            },
          ],
        },
      }),
    })
  })
  await page.goto('/families')

  await expect(page.getByText('新会话家长', { exact: true })).toBeVisible()
  await expect(page.getByText('退出前家长', { exact: true })).toHaveCount(0)
  expect(familyRequests).toBeGreaterThan(0)
})

test('ignores a late feature response after another feature is selected', async ({ page }) => {
  await useAdminSession(page)
  const lateAlpha = featurePayload(
    'feature_alpha',
    '迟到的旧功能',
    'conversation',
    '2026-10-18T09:00:00Z',
  )
  lateAlpha.description = '这条响应不应覆盖当前选择'
  const alpha = featurePayload(
    'feature_alpha',
    '缓存的语音功能',
    'conversation',
    '2026-10-18T09:00:00Z',
  )
  const beta = featurePayload('feature_beta', '当前内容功能', 'content', '2026-10-18T10:00:00Z')
  await page.route(/\/api\/v1\/admin\/feature-center$/, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { features: [beta, alpha] } }),
    })
  })
  await page.route(/\/api\/v1\/admin\/feature-center\/feature_alpha$/, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1200))
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { feature: lateAlpha } }),
    })
  })
  await page.route(/\/api\/v1\/admin\/feature-center\/feature_beta$/, async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { feature: beta } }),
    })
  })

  await page.goto('/feature-center')
  await expect(page.getByRole('heading', { name: '当前内容功能' })).toBeVisible()
  await page.getByRole('button', { name: /缓存的语音功能/ }).click()
  await page.getByRole('button', { name: /当前内容功能/ }).click()
  await expect(page.getByRole('heading', { name: '当前内容功能' })).toBeVisible()
  await page.waitForTimeout(1600)

  await expect(page.getByRole('heading', { name: '当前内容功能' })).toBeVisible()
  await expect(page.getByText('迟到的旧功能')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /迟到的旧功能/ })).toHaveCount(0)
})

test('debounces notification search and preserves visible support errors', async ({ page }) => {
  await useAdminSession(page)
  const queries: string[] = []
  await page.route('**/api/v1/admin/notifications**', async (route) => {
    const requestUrl = new URL(route.request().url())
    if (requestUrl.pathname.endsWith('/stats')) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'temporarily_unavailable', message: '统计暂时无法读取' },
        }),
      })
      return
    }
    queries.push(requestUrl.searchParams.get('query') ?? '')
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          items: [
            {
              id: `notice-${queries.length}`,
              category: 'system_announcement',
              severity: 'info',
              title: '搜索测试通知',
              body: '用于验证搜索防抖',
              action_path: '',
              action_label: '',
              audience: 'all_parents',
              parent_account_id: '',
              device_id: '',
              display_duration_seconds: 0,
              channels: ['in_app'],
              source: 'administrator',
              created_by: 'test-admin',
              publish_at: 'not-a-date',
              expires_at: '',
              created_at: '2026-10-18T09:00:00Z',
              updated_at: '2026-10-18T09:00:00Z',
              delivery_total: 1,
              delivery_pending: 0,
              delivery_delivered: 1,
              delivery_read: 0,
              delivery_failed: 0,
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'temporarily_unavailable', message: '家长名单暂时无法读取' },
      }),
    })
  })

  await page.goto('/notifications')
  await expect(page.getByText('时间待确认')).toBeVisible()
  await expect(page.getByText('通知统计暂时无法读取')).toBeVisible()
  await expect(page.getByRole('button', { name: '重新读取统计' })).toBeVisible()

  queries.length = 0
  const search = page.getByPlaceholder('搜索标题或内容')
  await search.pressSequentially('语音', { delay: 40 })
  await page.waitForTimeout(180)
  expect(queries).toEqual([])
  await page.waitForTimeout(220)
  expect(queries).toEqual(['语音'])

  await page.getByRole('button', { name: '发布通知' }).click()
  await page
    .getByRole('group', { name: '接收对象' })
    .getByRole('button', { name: '指定家长' })
    .click()
  const composer = page.getByRole('dialog', { name: '发布通知' })
  await expect(composer.getByText(/服务暂时不可用，请稍后重试/)).toBeVisible()
  await expect(page.getByRole('button', { name: '重新读取家长' })).toBeVisible()
})

test('keeps the newest device OTA release selected after an older detail arrives late', async ({
  page,
}) => {
  await useAdminSession(page)
  const release = (releaseId: string, firmwareVersion: string, signatureKeyId: string) => ({
    release_id: releaseId,
    firmware_version: firmwareVersion,
    hardware_revision: 'rev-a',
    channel: 'stable',
    status: 'draft',
    artifact_key: `firmware/${firmwareVersion}.bin`,
    artifact_url: `https://download.example.test/${firmwareVersion}.bin`,
    sha256: 'a'.repeat(64),
    size_bytes: 1024,
    signature_key_id: signatureKeyId,
    signature_algorithm: 'ed25519',
    signature: 'signed',
    signature_status: 'verified',
    rollback_allowed: true,
    min_source_version: '0.0.1',
    published_at: '',
    canary_percent: 0,
    target_type: 'all',
    target_id: '',
    release_notes: `${firmwareVersion} 说明`,
    record_version: 1,
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
  })
  const oldRelease = release('release-old', '1.0.0', 'old-key')
  const currentRelease = release('release-current', '2.0.0', 'current-key')
  await page.route('**/api/v1/admin/ota/releases**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/admin/ota/releases') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            releases: [oldRelease, currentRelease],
            total: 2,
            page: 1,
            page_size: 20,
          },
        }),
      })
      return
    }
    if (path.endsWith('/release-old')) {
      await new Promise((resolve) => setTimeout(resolve, 1200))
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            release: oldRelease,
            versions: [oldRelease],
            rollback: { allowed: true, target_version: '0.9.0' },
          },
        }),
      })
      return
    }
    if (path.endsWith('/release-current')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            release: currentRelease,
            versions: [currentRelease],
            rollback: { allowed: true, target_version: '1.0.0' },
          },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: { deployments: [], total: 0, page: 1, page_size: 20 },
      }),
    })
  })
  await page.route('**/api/v1/admin/ota/statistics', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          published_release_count: 0,
          canary_release_count: 0,
          active_deployment_count: 0,
          succeeded_deployment_count: 0,
          failed_deployment_count: 0,
          rollback_count: 0,
          failure_rate: 0,
          rollback_rate: 0,
        },
      }),
    })
  })

  await page.goto('/device-ota')
  await page.waitForSelector('button[aria-label="查看 1.0.0 详情"]')
  await page.evaluate(() => {
    const oldRelease = document.querySelector<HTMLButtonElement>(
      'button[aria-label="查看 1.0.0 详情"]',
    )
    const currentRelease = document.querySelector<HTMLButtonElement>(
      'button[aria-label="查看 2.0.0 详情"]',
    )
    if (oldRelease === null || currentRelease === null) {
      throw new Error('未找到固件版本详情按钮')
    }
    oldRelease.click()
    currentRelease.click()
  })
  const detailDialog = page.getByRole('dialog', { name: '固件版本详情' })
  await expect(detailDialog.getByRole('heading', { name: '2.0.0' })).toBeVisible()
  await page.waitForTimeout(1600)

  await expect(detailDialog.getByRole('heading', { name: '2.0.0' })).toBeVisible()
  await expect(detailDialog.getByText('current-key')).toBeVisible()
  await expect(detailDialog.getByText('old-key')).toHaveCount(0)
})

test('keeps the newest content package open after an older detail arrives late', async ({
  page,
}) => {
  await useAdminSession(page)
  const packageVersion = (packageId: string, title: string) => ({
    package_id: packageId,
    package_version: 1,
    title,
    category: 'story',
    age_tiers: ['age_3_4'],
    asset_key: `1.0.0/stable/all/resource/${packageId}.zip`,
    sha256: 'a'.repeat(64),
    size_bytes: 1024,
    status: 'draft',
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
  })
  const oldPackage = packageVersion('content_story_001', '旧内容包')
  const currentPackage = packageVersion('content_story_002', '当前内容包')
  await page.route('**/api/v1/admin/content/packages**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/admin/content/packages') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            packages: [oldPackage, currentPackage],
            page: 1,
            page_size: 20,
            total: 2,
          },
        }),
      })
      return
    }
    const packageId = path.endsWith('content_story_001')
      ? oldPackage.package_id
      : currentPackage.package_id
    if (packageId === oldPackage.package_id) {
      await new Promise((resolve) => setTimeout(resolve, 1200))
    }
    const version = packageId === oldPackage.package_id ? oldPackage : currentPackage
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          package_id: packageId,
          versions: [version],
          history: [],
        },
      }),
    })
  })

  await page.goto('/content')
  await page.waitForFunction(() => {
    return document.querySelectorAll('table tbody tr button').length >= 2
  })
  await page.evaluate(() => {
    const rows = document.querySelectorAll<HTMLTableRowElement>('table tbody tr')
    const manageButtons = Array.from(rows)
      .map((row) => row.querySelector<HTMLButtonElement>('button'))
      .filter((button): button is HTMLButtonElement => button?.textContent?.trim() === '管理')
    if (manageButtons.length < 2) {
      throw new Error('未找到两个内容包管理按钮')
    }
    manageButtons[0]!.click()
    manageButtons[1]!.click()
  })
  const drawer = page.getByRole('dialog', { name: '当前内容包' })
  await expect(drawer).toBeVisible()
  await page.waitForTimeout(1600)

  await expect(page.getByRole('dialog', { name: '当前内容包' })).toBeVisible()
  await expect(page.getByRole('dialog', { name: '旧内容包' })).toHaveCount(0)
})

test('keeps the newest service operation selected after an older detail arrives late', async ({
  page,
}) => {
  await useAdminSession(page)
  const operation = (id: string, message: string) => ({
    id,
    target_service: 'admin_web',
    current_version: '0.22.0',
    target_version: '0.23.0',
    status: 'queued',
    message,
    started_at: '2026-10-18T09:00:00Z',
    finished_at: '',
    log_tail: message,
  })
  const oldOperation = operation('operation-old', '旧升级记录')
  const currentOperation = operation('operation-current', '当前升级记录')
  await page.route('**/api/v1/admin/service-versions**', async (route) => {
    const requestUrl = new URL(route.request().url())
    if (requestUrl.pathname.endsWith('/admin_web/releases')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: { releases: [], page: 1, page_size: 100 },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'admin_web',
              display_name: '品牌管理端',
              role: '管理品牌',
              image: 'ghcr.io/test/admin-web:0.22.0',
              current_version: '0.22.0',
              latest_version: '0.23.0',
              status: 'outdated',
              release_url: '',
              is_self: true,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: true,
              last_checked_at: '2026-10-18T09:00:00Z',
              updated_at: '2026-10-18T09:00:00Z',
            },
          ],
          checked_at: '2026-10-18T09:00:00Z',
          all_up_to_date: false,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: { operations: [oldOperation, currentOperation] },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations/operation-old', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1200))
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operation: oldOperation } }),
    })
  })
  await page.route(
    '**/api/v1/admin/service-version-operations/operation-current',
    async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data: { operation: currentOperation } }),
      })
    },
  )

  await page.goto('/services')
  await page.waitForFunction(() => {
    return (
      Array.from(document.querySelectorAll('button')).filter(
        (button) => button.textContent?.trim() === '查看进度',
      ).length >= 2
    )
  })
  await page.evaluate(() => {
    const progressButtons = Array.from(document.querySelectorAll('button')).filter(
      (button) => button.textContent?.trim() === '查看进度',
    )
    if (progressButtons.length < 2) {
      throw new Error('未找到两个升级进度按钮')
    }
    progressButtons[0]!.click()
    progressButtons[1]!.click()
  })
  const dialog = page.getByRole('dialog')
  await expect(dialog.locator('.operation-message')).toHaveText('当前升级记录')
  await page.waitForTimeout(1600)

  await expect(dialog.locator('.operation-message')).toHaveText('当前升级记录')
  await expect(dialog.locator('.operation-message')).not.toHaveText('旧升级记录')
})

test('shows the invalid verification code instead of a generic error', async ({ page }) => {
  await page.route('**/api/v1/admin/auth/login', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: '1.0.0',
        request_id: 'test-login',
        data: {
          status: 'mfa_required',
          challenge_token: 'test-challenge',
          account: {
            id: 'test-admin',
            email: 'admin@sprout.local',
            display_name: '管理员',
            role: 'admin',
            status: 'active',
          },
        },
        error: null,
      }),
    })
  })
  await page.route('**/api/v1/admin/auth/mfa', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: '1.0.0',
        request_id: 'test-mfa',
        data: null,
        error: {
          code: 'invalid_mfa_code',
          message: '验证码不正确，请重新输入',
          retryable: false,
        },
      }),
    })
  })

  await page.goto('/login')
  await page.getByLabel('管理员邮箱').fill('admin@sprout.local')
  await page.getByLabel('密码').fill('test-password')
  await page.getByRole('button', { name: '登录管理后台' }).click()
  await page.getByLabel('6 位验证码').fill('123456')
  await page.getByRole('button', { name: '验证并登录' }).click()

  await expect(page.getByText('验证码不正确，请重新输入')).toBeVisible()
  await expect(page.getByText('操作没有完成，请稍后重试')).toHaveCount(0)
})

test('uses the fastest available model and the provider default balance', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/settings', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            settings: {
              ai: {
                default_balance_usd: 0,
                default_concurrency: 1,
                default_models: [],
              },
              account: {},
              safety: {},
              update: {},
              retention: {},
            },
          },
        }),
      })
      return
    }
    await route.continue()
  })
  await page.route('**/api/v1/admin/ai-account-defaults', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          default_balance_usd: 6.5,
          default_concurrency: 2,
          source: 'sub2api',
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          models: [
            { id: 'slow-model', latency_ms: 500 },
            { id: 'fast-model', latency_ms: 80 },
          ],
        },
      }),
    })
  })

  await page.goto('/settings')

  await expect(page.getByLabel('初始额度')).toHaveValue('6.5')
  await expect(page.getByRole('checkbox', { name: /fast-model/ })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: /slow-model/ })).not.toBeChecked()
})

test('keeps the saved default models when another setting is saved', async ({ page }) => {
  await useAdminSession(page)

  let savedDefaultModels: string[] = []
  await page.route('**/api/v1/admin/settings', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            version: 4,
            settings: {
              ai: {
                default_balance_usd: 10,
                default_concurrency: 3,
                default_models: ['slow-model', 'legacy-model'],
              },
              account: {},
              safety: {},
              voice: {},
              update: {},
              retention: {},
            },
          },
        }),
      })
      return
    }
    const payload = route.request().postDataJSON() as {
      settings: { ai: { default_models: string[] } }
      expected_version: number
    }
    savedDefaultModels = payload.settings.ai.default_models
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          version: 5,
          settings: {
            ...payload.settings,
          },
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-account-defaults', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          default_balance_usd: 10,
          default_concurrency: 3,
          source: 'sub2api',
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          models: [
            { id: 'slow-model', latency_ms: 500 },
            { id: 'fast-model', latency_ms: 80 },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-gateway/child-policy', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })

  await page.goto('/settings')

  await expect(page.getByRole('checkbox', { name: /slow-model/ })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: /legacy-model/ })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: /fast-model/ })).not.toBeChecked()

  await page.getByLabel('默认同时对话数量').fill('7')
  await page.locator('header').getByRole('button', { name: '保存系统设置' }).click()

  await expect(page.getByText('系统设置已保存。')).toBeVisible()
  expect(savedDefaultModels).toEqual(['slow-model', 'legacy-model'])
  await expect(page.getByRole('checkbox', { name: /slow-model/ })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: /legacy-model/ })).toBeChecked()
  await expect(page.getByRole('checkbox', { name: /fast-model/ })).not.toBeChecked()
})

test('keeps system settings writable only for settings.manage', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['settings.read'])

  let writeRequests = 0
  await page.route('**/api/v1/admin/settings', async (route) => {
    if (route.request().method() !== 'GET') {
      writeRequests += 1
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          version: 3,
          settings: {
            ai: {
              default_balance_usd: 10,
              default_concurrency: 4,
              default_models: ['gpt-safe'],
            },
            account: {},
            safety: {},
            voice: {},
            update: {},
            retention: {},
          },
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai/**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })

  await page.goto('/settings')

  await expect(
    page.getByText('你当前只有查看权限。可以查看系统设置，但保存和修改控件已锁定。'),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '保存系统设置' })).toHaveCount(0)
  await expect(page.getByLabel('默认同时对话数量')).toBeDisabled()
  await expect(page.getByRole('checkbox', { name: /gpt-safe/ })).toBeDisabled()
  expect(writeRequests).toBe(0)
})

test('manages a family AI account, profile, password, and bound devices', async ({ page }) => {
  await useAdminSession(page)
  const modelRequests: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/admin/ai-models')) {
      modelRequests.push(request.url())
    }
  })

  const family = {
    parent_account_id: 'parent-1',
    phone: '13273330085',
    display_name: '测试家长',
    guardian_family_name: '王',
    child_nickname: '小芽',
    child_birthday: '2020-05-06',
    status: 'active',
    created_at: '2026-10-01T10:00:00Z',
    ai_account: {
      provider_account_id: 'provider-1',
      status: 'active',
      balance_usd: 0,
      concurrency_limit: 1,
      available_models: ['slow-model', 'fast-model'],
      selected_models: ['fast-model'],
      allowed_models: ['slow-model', 'fast-model'],
      credential_ready: true,
      updated_at: '2026-10-03T10:00:00Z',
    },
  }

  await page.route('**/api/v1/admin/families**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [family],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          models: [
            { id: 'slow-model', latency_ms: 500 },
            { id: 'fast-model', latency_ms: 80 },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/devices', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          devices: [
            {
              device_id: 'device-1',
              device_name: '初芽一号',
              hardware_model: '初芽标准版',
              firmware_version: '0.11.0',
              capabilities: ['display', 'microphone', 'wifi'],
              bound_at: '2026-10-02T10:00:00Z',
              updated_at: '2026-10-03T10:00:00Z',
              runtime: {
                is_online: true,
                reported_at: '2026-10-03T10:00:00Z',
                received_at: '2026-10-03T10:00:02Z',
                connection: {
                  state: 'connected',
                  transport: 'wifi',
                },
              },
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/ai-accounts/provider-1', async (route) => {
    const request = route.request()
    expect(request.method()).toBe('PUT')
    expect(request.postDataJSON()).toMatchObject({
      balance_usd: 12.5,
      concurrency_limit: 2,
      available_models: ['slow-model', 'fast-model'],
    })
    family.ai_account.balance_usd = 12.5
    family.ai_account.concurrency_limit = 2
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          ...family.ai_account,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/ai-usage', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          provider_account_id: 'provider-1',
          start_time: '2026-10-02T10:00:00Z',
          end_time: '2026-10-03T10:00:00Z',
          totals: {
            requests: 12,
            input_tokens: 1000,
            output_tokens: 2000,
            cache_creation_tokens: 0,
            cache_read_tokens: 0,
            total_tokens: 3000,
            cost_usd: 0.5,
            actual_cost_usd: 0.45,
            average_duration_ms: 250,
          },
          models: [
            {
              model: 'fast-model',
              requests: 12,
              input_tokens: 1000,
              output_tokens: 2000,
              cache_creation_tokens: 0,
              cache_read_tokens: 0,
              total_tokens: 3000,
              cost_usd: 0.5,
              actual_cost_usd: 0.45,
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/profile', async (route) => {
    const request = route.request()
    expect(request.method()).toBe('PUT')
    const payload = request.postDataJSON()
    Object.assign(family, {
      display_name: payload.display_name,
      guardian_family_name: payload.guardian_family_name,
      child_nickname: payload.child_nickname,
      child_birthday: payload.child_birthday,
    })
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/password', async (route) => {
    expect(route.request().postDataJSON()).toEqual({ password: 'new-secret-123' })
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/members', async (route) => {
    expect(route.request().method()).toBe('GET')
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          membership_revision: 3,
          members: [
            {
              id: 'member-owner',
              family_id: 'family-1',
              user_id: 'user-owner',
              role: 'owner',
              display_name: '王',
              account_role: 'parent',
              account_status: 'active',
              created_at: '2026-10-01T10:00:00Z',
              updated_at: '2026-10-01T10:00:00Z',
            },
            {
              id: 'member-guardian',
              family_id: 'family-1',
              user_id: 'user-guardian',
              role: 'guardian',
              display_name: '李',
              account_role: 'parent',
              account_status: 'active',
              created_at: '2026-10-01T11:00:00Z',
              updated_at: '2026-10-01T11:00:00Z',
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/members/user-guardian', async (route) => {
    expect(route.request().method()).toBe('PUT')
    expect(route.request().headers()['if-match']).toBe('3')
    expect(route.request().postDataJSON()).toEqual({
      role: 'viewer',
    })
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          member: {
            id: 'member-guardian',
            family_id: 'family-1',
            user_id: 'user-guardian',
            role: 'viewer',
          },
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-1/invitations', async (route) => {
    const request = route.request()
    if (request.method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            membership_revision: 3,
            invitations: [
              {
                id: 'invitation-1',
                family_id: 'family-1',
                email: 'guardian@example.com',
                role: 'guardian',
                status: 'pending',
                invited_by_user_id: 'user-owner',
                created_at: '2026-10-02T10:00:00Z',
                expires_at: '2026-10-09T10:00:00Z',
                accepted_at: null,
              },
            ],
          },
        }),
      })
      return
    }
    expect(request.method()).toBe('POST')
    expect(request.headers()['if-match']).toBe('3')
    expect(request.postDataJSON()).toEqual({
      email: 'second@example.com',
      role: 'viewer',
    })
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          invitation: {
            id: 'invitation-2',
            family_id: 'family-1',
            email: 'second@example.com',
            role: 'viewer',
            status: 'pending',
            invited_by_user_id: 'user-owner',
            created_at: '2026-10-03T10:00:00Z',
            expires_at: '2026-10-10T10:00:00Z',
            accepted_at: null,
          },
        },
      }),
    })
  })

  await page.goto('/families')

  await expect(page.getByRole('heading', { name: '家长账号' })).toBeVisible()
  const familyRow = page.getByRole('row', { name: /王.*13273330085/ })
  await expect(familyRow.getByText(/额度\s+0\.00/)).toBeVisible()

  await page.getByRole('button', { name: '管理' }).click()

  await expect(page.getByRole('dialog', { name: '王' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '家长资料' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'AI 服务' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'AI 用量核对' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '家庭成员与邀请' })).toBeVisible()
  await expect(page.getByText('守护者', { exact: true })).toBeVisible()
  await expect(page.getByText('查看者', { exact: true })).toBeVisible()
  await expect(page.getByText('guardian@example.com')).toBeVisible()
  await expect(page.getByText('等待加入', { exact: true })).toBeVisible()
  await expect(page.getByText('管理员不能代替家长创建邀请', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: '绑定设备' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '账号安全' })).toBeVisible()
  await expect(page.getByText('正在读取可用模型…')).toHaveCount(0)
  expect(modelRequests).not.toHaveLength(0)
  await expect(page.getByText('默认最快')).toBeVisible()
  await expect(page.getByText('12 次')).toBeVisible()
  await expect(page.getByText('0.4500', { exact: true }).first()).toBeVisible()
  await expect(page.getByText(/\$|美元/)).toHaveCount(0)
  await expect(page.getByText('初芽一号')).toBeVisible()
  await expect(page.getByText('在线', { exact: true })).toBeVisible()

  const modelCheckboxes = page.locator('.model-option input[type="checkbox"]')
  await expect(modelCheckboxes).toHaveCount(2)
  await expect(modelCheckboxes.nth(0)).toBeChecked()
  await expect(modelCheckboxes.nth(1)).toBeChecked()

  await page.getByLabel('剩余额度').fill('12.5')
  await page.getByLabel('同时对话数量').fill('2')
  await page.getByRole('button', { name: '保存 AI 设置' }).click()
  await expect(page.getByText('AI 设置已保存。')).toBeVisible()

  await page.getByLabel('家长称呼').fill('更新后的家长')
  await page.getByLabel('家长姓氏').fill('李')
  await page.getByLabel('宝贝姓名').fill('新芽')
  await page.getByLabel('宝贝生日').fill('2021-06-07')
  await page.getByRole('button', { name: '保存家长资料' }).click()
  await expect(page.getByText('家长资料已保存。')).toBeVisible()

  await page.getByRole('button', { name: '重置密码' }).click()
  const resetDialog = page.getByRole('dialog', { name: '重置家长密码' })
  await resetDialog.getByLabel('新密码', { exact: true }).fill('new-secret-123')
  await resetDialog.getByLabel('再次输入新密码').fill('new-secret-123')
  await resetDialog.getByRole('button', { name: '重置密码' }).click()
  await expect(page.getByText('密码已重置，该家长需要重新登录。')).toBeVisible()

  await page.getByLabel('调整 李 的家庭角色').selectOption('viewer')
  await page.getByRole('button', { name: '保存角色' }).click()
  const roleDialog = page.getByRole('dialog', { name: '确认调整家庭角色' })
  await expect(roleDialog).toBeVisible()
  await roleDialog.getByRole('button', { name: '保存新角色' }).click()
  await expect(page.getByText('成员角色已更新。')).toBeVisible()

  await page.getByLabel('邀请邮箱').fill('second@example.com')
  await page.getByLabel('加入后的角色', { exact: true }).selectOption('viewer')
  await page.getByRole('button', { name: '发出邀请' }).click()
  await expect(page.getByText('邀请已发出。')).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  const drawer = page.locator('.family-drawer')
  await expect(drawer).toBeVisible()
  const drawerBox = await drawer.boundingBox()
  expect(drawerBox?.width).toBeLessThanOrEqual(390)

  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByRole('row', { name: /李.*13273330085/ })).toContainText('额度 12.50')

  await page.getByRole('button', { name: '管理' }).click()
  await expect(page.getByLabel('剩余额度')).toHaveValue('12.5')
  await expect(page.getByLabel('家长称呼')).toHaveValue('更新后的家长')
})

test('keeps download controls hidden and readonly without storage.manage', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['storage.read'])

  let writeRequests = 0
  await page.route('**/api/v1/admin/storage/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() !== 'GET') {
      writeRequests += 1
    }
    if (path.endsWith('/storage/files')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            files: [
              {
                id: 'file-1',
                name: '初芽-0.23.0.apk',
                relative_path: '0.23.0/stable/android/apk/初芽-0.23.0.apk',
                directory: '0.23.0/stable/android/apk',
                size_bytes: 1024,
                modified_at: '2026-10-18T09:00:00Z',
                sha256: 'a'.repeat(64),
                version: '0.23.0',
                kind: 'client',
                platform: 'android',
                download_url: 'https://download.example.test/0.23.0/app.apk',
                is_indexed: true,
              },
            ],
          },
        }),
      })
      return
    }
    if (path.endsWith('/storage/index/status')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            is_available: true,
            refreshed_at: '2026-10-18T09:05:00Z',
            indexed_file_count: 1,
            pending_file_count: 0,
          },
        }),
      })
      return
    }
    await route.fulfill({ status: 404 })
  })

  await page.goto('/downloads')

  await expect(
    page.getByText(
      '你当前只有查看权限。可以查看下载服务器上的文件，但不能上传、删除或刷新发布索引。',
    ),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '上传文件' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新发布索引' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '删除' })).toHaveCount(0)
  await expect(page.getByText('只读', { exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: '前往内容发布' })).toHaveCount(0)
  expect(writeRequests).toBe(0)
})

test('fills the current version and the release artifact automatically', async ({ page }) => {
  await useAdminSession(page)
  let artifactChannel = ''
  await page.route('**/api/v1/admin/releases', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { releases: [] } }),
    })
  })
  await page.route('**/api/v1/admin/release-artifacts/*', async (route) => {
    artifactChannel = new URL(route.request().url()).searchParams.get('channel') ?? ''
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          artifact: {
            version: '0.8.1',
            kind: 'client',
            platform: 'android',
            download_url: 'https://downloads.example.com/0.8.1/app.apk',
            sha256: 'a'.repeat(64),
          },
        },
      }),
    })
  })

  await page.goto('/releases')
  await page.locator('header').getByRole('button', { name: '登记新版本' }).click()
  await expect(page.getByLabel('版本号')).toHaveValue(/\d+\.\d+\.\d+/)
  await page.getByRole('button', { name: '自动查找更新文件' }).click()

  expect(artifactChannel).toBe('stable')
  await expect(page.getByLabel('下载地址')).toHaveValue(
    'https://downloads.example.com/0.8.1/app.apk',
  )
  await expect(page.getByLabel('文件校验值')).toHaveValue('a'.repeat(64))
})

test('checks and upgrades brand services from the service version page', async ({ page }) => {
  await useAdminSession(page)

  const operation = {
    id: 'operation-admin-web',
    target_service: 'admin_web',
    current_version: '0.9.0',
    target_version: '0.9.1',
    status: 'queued',
    message: '升级任务已排队。',
    started_at: '2026-10-03T10:00:00Z',
    finished_at: '',
    log_tail: '已创建升级任务。',
  }
  let serviceSnapshotRequests = 0
  const releaseRequests: string[] = []
  const upgradeableServiceIds = new Set([
    'sub2api',
    'device_platform',
    'voice_gateway',
    'admin_web',
  ])
  const infrastructureServiceIds = new Set([
    'postgres',
    'redis',
    'mqtt',
    'download_http',
    'download_ftp',
  ])
  const serviceVersions = [
    {
      id: 'admin_web',
      display_name: '品牌管理端',
      role: '管理员查看品牌数据并管理服务',
      image: 'ghcr.io/tissyboxc/sprout-admin-web:0.9.0',
      current_version: '0.9.0',
      latest_version: '0.9.1',
      status: 'outdated',
      release_url: 'https://github.com/TissyBoxC/sprout-platform/releases/tag/v0.9.1',
      is_self: true,
      upgrade_supported: true,
      upgrade_executable: true,
      can_upgrade: true,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'sub2api',
      display_name: 'AI 服务',
      role: '为设备提供 AI 能力',
      image: 'ghcr.io/tissyboxc/sub2api:0.2.15',
      current_version: '0.2.15',
      latest_version: '0.2.16',
      status: 'outdated',
      release_url: 'https://github.com/TissyBoxC/sprout-sub2api-fork/releases/tag/v0.2.16',
      is_self: false,
      upgrade_supported: true,
      upgrade_executable: true,
      can_upgrade: true,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'device_platform',
      display_name: '设备平台',
      role: '管理设备连接和家庭绑定',
      image: 'ghcr.io/tissyboxc/sprout-device-platform:0.9.0',
      current_version: '0.9.0',
      latest_version: '0.9.1',
      status: 'outdated',
      release_url: 'https://github.com/TissyBoxC/sprout-platform/releases/tag/v0.9.1',
      is_self: false,
      upgrade_supported: true,
      upgrade_executable: true,
      can_upgrade: true,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'voice_gateway',
      display_name: '语音服务',
      role: '处理设备语音对话',
      image: 'ghcr.io/tissyboxc/sprout-voice-gateway:0.9.0',
      current_version: '0.9.0',
      latest_version: '0.9.1',
      status: 'outdated',
      release_url: 'https://github.com/TissyBoxC/sprout-platform/releases/tag/v0.9.1',
      is_self: false,
      upgrade_supported: true,
      upgrade_executable: true,
      can_upgrade: true,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'postgres',
      display_name: '数据库',
      role: '保存平台数据',
      image: 'postgres:16-alpine',
      current_version: '16',
      latest_version: '16',
      status: 'current',
      release_url: '',
      is_self: false,
      upgrade_supported: false,
      upgrade_executable: false,
      can_upgrade: false,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'redis',
      display_name: '缓存服务',
      role: '缓存平台运行数据',
      image: 'redis:7-alpine',
      current_version: '7',
      latest_version: '7',
      status: 'current',
      release_url: '',
      is_self: false,
      upgrade_supported: false,
      upgrade_executable: false,
      can_upgrade: false,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'mqtt',
      display_name: '设备消息服务',
      role: '连接设备与平台',
      image: 'eclipse-mosquitto:2',
      current_version: '2',
      latest_version: '2',
      status: 'current',
      release_url: '',
      is_self: false,
      upgrade_supported: false,
      upgrade_executable: false,
      can_upgrade: false,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'download_http',
      display_name: '下载服务',
      role: '提供设备更新下载',
      image: 'nginx:1.27-alpine',
      current_version: '1.27',
      latest_version: '1.28',
      status: 'outdated',
      release_url: '',
      is_self: false,
      upgrade_supported: false,
      upgrade_executable: false,
      can_upgrade: true,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
    {
      id: 'download_ftp',
      display_name: '发布文件服务',
      role: '接收新版本发布文件',
      image: 'atmoz/sftp:alpine',
      current_version: 'alpine',
      latest_version: 'alpine',
      status: 'current',
      release_url: '',
      is_self: false,
      upgrade_supported: false,
      upgrade_executable: false,
      can_upgrade: false,
      last_checked_at: '2026-10-03T10:00:00Z',
      updated_at: '2026-10-03T10:00:00Z',
    },
  ]

  await page.route('**/api/v1/admin/service-versions', async (route) => {
    serviceSnapshotRequests += 1
    const checkedAt =
      serviceSnapshotRequests === 1 ? '2026-10-03T10:00:00Z' : '2026-10-03T10:01:00Z'
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: serviceVersions.map((service) => ({
            ...service,
            last_checked_at: checkedAt,
            updated_at: checkedAt,
          })),
          checked_at: checkedAt,
          all_up_to_date: false,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/check', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: serviceVersions.map((service) => ({
            ...service,
            last_checked_at: '2026-10-03T10:01:00Z',
            updated_at: '2026-10-03T10:01:00Z',
          })),
          checked_at: '2026-10-03T10:01:00Z',
          all_up_to_date: false,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    const requestUrl = new URL(route.request().url())
    const serviceId = requestUrl.pathname.split('/').at(-2) ?? ''
    releaseRequests.push(route.request().url())
    if (!upgradeableServiceIds.has(serviceId)) {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ message: 'not found' }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: serviceId,
          releases: [
            {
              version:
                serviceId === 'admin_web' ? '0.9.1' : serviceId === 'sub2api' ? '0.2.16' : '0.9.1',
              release_url: 'https://github.com/TissyBoxC/sprout-platform/releases/tag/v0.9.1',
              published_at: '2026-10-03T10:00:00Z',
              is_current: false,
              is_latest: true,
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/upgrade', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operation } }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/admin_web/upgrade', async (route) => {
    const request = route.request()
    expect(request.postDataJSON()).toEqual({ target_version: '0.9.1' })
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operation } }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [operation] } }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations/*', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operation } }),
    })
  })

  await page.goto('/services')

  await expect(page.getByRole('heading', { name: '服务版本' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '全部服务' })).toBeVisible()
  await expect(page.locator('table')).toHaveCount(1)
  await expect(page.locator('table tbody tr')).toHaveCount(10)
  await expect(page.locator('.time-value')).toHaveText(/2026/)
  await expect(page.getByText('只读服务')).toHaveCount(0)
  await expect(page.getByText('管理端自身', { exact: true })).toBeVisible()
  await expect(page.getByText('固定镜像，由部署配置统一维护').first()).toBeVisible()
  await expect(page.getByText('管理端自身升级时，页面会短暂重载，完成后自动恢复')).toBeVisible()
  await expect(page.getByText('0.9.0', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: '检查更新' }).click()
  await expect(page.getByText('检查完成，有 4 个服务可以升级。')).toBeVisible()
  expect(serviceSnapshotRequests).toBeGreaterThan(0)
  const requestedReleaseServiceIds = new Set(
    releaseRequests.map((url) => new URL(url).pathname.split('/').at(-2) ?? ''),
  )
  expect(requestedReleaseServiceIds).toEqual(upgradeableServiceIds)
  expect(
    [...requestedReleaseServiceIds].some((serviceId) => infrastructureServiceIds.has(serviceId)),
  ).toBe(false)
  expect(releaseRequests.every((url) => new URL(url).searchParams.get('page_size') === '100')).toBe(
    true,
  )

  await expect(page.getByLabel('品牌管理端版本')).toHaveValue('0.9.1')
  await page
    .getByRole('row', { name: /品牌管理端/ })
    .getByRole('button', { name: '升级到 0.9.1' })
    .click()

  await expect(page.getByText('品牌管理端 的升级已开始。')).toBeVisible()
  await expect(page.getByRole('button', { name: '查看进度' })).toBeVisible()
})

test('keeps the previous service snapshot when polling times out and retries successfully', async ({
  page,
}) => {
  await useAdminSession(page)

  let checkAttempts = 0
  let staleLoadsRemaining = 0
  let checkedSnapshot = {
    checked_at: '2026-10-16T10:00:00Z',
    services: [
      {
        id: 'admin_web',
        display_name: '品牌管理端',
        role: '平台服务',
        image: 'ghcr.io/tissyboxc/sprout-admin-web:0.22.0',
        current_version: '0.22.0',
        latest_version: '0.22.1',
        status: 'outdated',
        release_url: '',
        is_self: true,
        upgrade_supported: true,
        upgrade_executable: true,
        can_upgrade: true,
        last_checked_at: '2026-10-16T10:00:00Z',
        updated_at: '2026-10-16T10:00:00Z',
      },
    ],
  }

  await page.route('**/api/v1/admin/service-versions', async (route) => {
    const responseSnapshot =
      staleLoadsRemaining > 0
        ? {
            checked_at: '2026-10-16T10:00:00Z',
            services: checkedSnapshot.services.map((service) => ({
              ...service,
              current_version: '0.22.0',
              latest_version: '0.22.1',
              status: 'outdated',
              can_upgrade: true,
              last_checked_at: '2026-10-16T10:00:00Z',
              updated_at: '2026-10-16T10:00:00Z',
            })),
          }
        : checkedSnapshot
    if (staleLoadsRemaining > 0) {
      staleLoadsRemaining -= 1
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          ...responseSnapshot,
          all_up_to_date: responseSnapshot.services.every(
            (service) => service.status === 'current',
          ),
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/check', async (route) => {
    checkAttempts += 1
    if (checkAttempts === 1) {
      staleLoadsRemaining = 10
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            checked_at: '2026-10-16T10:00:00Z',
            services: checkedSnapshot.services.map((service) => ({
              ...service,
              current_version: '0.22.0',
              latest_version: '0.22.1',
              status: 'outdated',
              can_upgrade: true,
              last_checked_at: '2026-10-16T10:00:00Z',
              updated_at: '2026-10-16T10:00:00Z',
            })),
            all_up_to_date: false,
            execution_enabled: true,
          },
        }),
      })
      return
    }

    checkedSnapshot = {
      checked_at: '2026-10-17T10:05:00Z',
      services: checkedSnapshot.services.map((service) => ({
        ...service,
        latest_version: '0.22.0',
        status: 'current',
        can_upgrade: false,
        last_checked_at: '2026-10-17T10:05:00Z',
        updated_at: '2026-10-17T10:05:00Z',
      })),
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          ...checkedSnapshot,
          all_up_to_date: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: 'admin_web',
          releases:
            checkedSnapshot.services[0].status === 'current'
              ? []
              : [
                  {
                    version: '0.22.1',
                    release_url: '',
                    published_at: '2026-10-16T10:00:00Z',
                    is_current: false,
                    is_latest: true,
                  },
                ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')
  await expect(page.getByText('0.22.1', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: '检查更新' }).click()

  await expect(page.getByRole('alert')).toContainText('检查尚未返回')
  await expect(page.getByText('检查尚未返回，正在显示上次检查结果。请稍后重新检查。')).toBeVisible()
  await expect(page.getByText('0.22.1', { exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '升级到 0.22.1' })).toBeDisabled()
  await expect(page.getByText('所有受管服务均已是最新版本，目前没有可执行的升级。')).toHaveCount(0)
  await expect(page.getByText('所有服务都已是当前版本。')).toHaveCount(0)

  await page.getByRole('button', { name: '重新检查' }).click()

  await expect(page.getByText('所有服务都已是当前版本。')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByText('所有受管服务均已是最新版本，目前没有可执行的升级。')).toBeVisible()
  expect(checkAttempts).toBe(2)
})

test('keeps a read-only service snapshot visible without offering upgrades', async ({ page }) => {
  await useAdminSession(page)

  let upgradeRequests = 0
  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'device_platform',
              display_name: '设备平台',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-device-platform:0.23.0',
              current_version: '0.22.0',
              latest_version: '0.23.0',
              status: 'outdated',
              release_url: '',
              is_self: false,
              upgrade_supported: true,
              upgrade_executable: false,
              can_upgrade: false,
              last_checked_at: '2026-10-17T10:00:00Z',
              updated_at: '2026-10-17T10:00:00Z',
            },
          ],
          checked_at: '2026-10-17T10:00:00Z',
          all_up_to_date: false,
          execution_enabled: false,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ data: null, error: { code: 'not_found' } }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/upgrade', async (route) => {
    upgradeRequests += 1
    await route.fulfill({ status: 500 })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')

  await expect(page.getByText('0.23.0', { exact: true })).toBeVisible()
  await expect(page.getByText('当前部署仅同步版本，不执行自动升级')).toBeVisible()
  await expect(page.getByRole('button', { name: '全部升级' })).toBeDisabled()
  await expect(page.getByRole('button', { name: /升级到/ })).toHaveCount(0)
  expect(upgradeRequests).toBe(0)
})

test('keeps service version controls readonly without service.upgrade', async ({ page }) => {
  await useAdminSessionWithPermissions(page, ['service.read'])

  let writeRequests = 0
  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'admin_web',
              display_name: '品牌管理端',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-admin-web:0.23.0',
              current_version: '0.23.0',
              latest_version: '0.23.1',
              status: 'outdated',
              release_url: '',
              is_self: true,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: true,
              last_checked_at: '2026-10-18T10:00:00Z',
              updated_at: '2026-10-18T10:00:00Z',
            },
          ],
          checked_at: '2026-10-18T10:00:00Z',
          all_up_to_date: false,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/check', async (route) => {
    writeRequests += 1
    await route.fulfill({ status: 500 })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: 'admin_web',
          releases: [
            {
              version: '0.23.1',
              release_url: '',
              published_at: '2026-10-18T09:00:00Z',
              is_current: false,
              is_latest: true,
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/upgrade', async (route) => {
    writeRequests += 1
    await route.fulfill({ status: 500 })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')

  await expect(
    page.getByText('你当前只有查看权限。可以查看版本状态，但不能选择版本或启动升级。'),
  ).toBeVisible()
  await expect(page.getByRole('button', { name: '检查更新' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新检查' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '全部升级' })).toBeDisabled()
  await expect(page.getByRole('button', { name: /升级到/ })).toHaveCount(0)
  await expect(page.getByLabel('品牌管理端版本')).toHaveCount(0)
  expect(writeRequests).toBe(0)
})

test('renders only the backend service catalog without fabricating worker rows', async ({
  page,
}) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'admin_web',
              display_name: '品牌管理端',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-admin-web:0.23.0',
              current_version: '0.23.0',
              latest_version: '0.23.0',
              status: 'current',
              release_url: '',
              is_self: true,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: false,
              last_checked_at: '2026-10-18T10:00:00Z',
              updated_at: '2026-10-18T10:00:00Z',
            },
          ],
          checked_at: '2026-10-18T10:00:00Z',
          all_up_to_date: true,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')

  await expect(page.getByRole('row', { name: /服务升级执行器/ })).toHaveCount(0)
  await expect(page.getByRole('row', { name: /品牌管理端/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /升级到/ })).toHaveCount(0)
})

test('distinguishes a release catalog that is still preparing from a real failure', async ({
  page,
}) => {
  await useAdminSession(page)

  let releaseRequests = 0
  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'device_platform',
              display_name: '设备平台',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-device-platform:0.17.0',
              current_version: '0.17.0',
              latest_version: '0.17.1',
              status: 'outdated',
              release_url: '',
              is_self: false,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: true,
              last_checked_at: '2026-10-08T10:00:00Z',
              updated_at: '2026-10-08T10:00:00Z',
            },
          ],
          checked_at: '2026-10-08T10:00:00Z',
          all_up_to_date: false,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    releaseRequests += 1
    if (releaseRequests === 1) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          data: null,
          error: {
            code: 'release_catalog_not_ready',
            message: '版本目录正在准备，稍后刷新即可',
            retryable: true,
          },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: 'device_platform',
          releases: [
            {
              version: '0.17.1',
              release_url: 'https://github.com/TissyBoxC/sprout-platform/releases/tag/v0.17.1',
              published_at: '2026-10-08T10:00:00Z',
              is_current: false,
              is_latest: true,
            },
          ],
        },
      }),
    })
  })

  await page.goto('/services')

  await expect(page.getByText('版本目录正在准备，稍后刷新即可')).toBeVisible()
  await expect(page.getByText('服务暂时不可用，请稍后重试')).toHaveCount(0)
  await page.getByRole('button', { name: '重新读取版本' }).click()
  await expect(page.getByLabel('设备平台版本')).toHaveValue('0.17.1')
  await expect(page.getByText('版本目录正在准备，稍后刷新即可')).toHaveCount(0)
  expect(releaseRequests).toBe(2)
})

test('shows an explicit no-upgrade state when all managed services are current', async ({
  page,
}) => {
  await useAdminSession(page)

  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'admin_web',
              display_name: '品牌管理端',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-admin-web:0.22.0',
              current_version: '0.22.0',
              latest_version: '0.22.0',
              status: 'current',
              release_url: '',
              is_self: true,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: false,
              last_checked_at: '2026-10-14T10:00:00Z',
              updated_at: '2026-10-14T10:00:00Z',
            },
          ],
          checked_at: '2026-10-14T10:00:00Z',
          all_up_to_date: true,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: 'admin_web',
          releases: [
            {
              version: '0.22.0',
              release_url: '',
              published_at: '2026-10-14T10:00:00Z',
              is_current: true,
              is_latest: true,
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')

  await expect(page.getByText('所有受管服务均已是最新版本，目前没有可执行的升级。')).toBeVisible()
  await expect(page.getByText('当前已是最新版本，无需升级').first()).toBeVisible()
  await expect(page.getByRole('button', { name: /^升级到/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '全部升级' })).toBeDisabled()
  await expect(page.getByLabel('品牌管理端版本')).toHaveCount(0)
})

test('explains when the backend reports that an upgrade is not needed', async ({ page }) => {
  await useAdminSession(page)

  await page.route('**/api/v1/admin/service-versions', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          services: [
            {
              id: 'admin_web',
              display_name: '品牌管理端',
              role: '平台服务',
              image: 'ghcr.io/tissyboxc/sprout-admin-web:0.22.0',
              current_version: '0.22.0',
              latest_version: '0.22.1',
              status: 'outdated',
              release_url: '',
              is_self: true,
              upgrade_supported: true,
              upgrade_executable: true,
              can_upgrade: true,
              last_checked_at: '2026-10-14T10:00:00Z',
              updated_at: '2026-10-14T10:00:00Z',
            },
          ],
          checked_at: '2026-10-14T10:00:00Z',
          all_up_to_date: false,
          execution_enabled: true,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/*/releases**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          service: 'admin_web',
          releases: [
            {
              version: '0.22.1',
              release_url: '',
              published_at: '2026-10-14T10:00:00Z',
              is_current: false,
              is_latest: true,
            },
          ],
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-versions/admin_web/upgrade', async (route) => {
    await route.fulfill({
      status: 422,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: '1.0.0',
        request_id: 'test-service-not-upgradable',
        data: null,
        error: {
          code: 'service_not_upgradable',
          message: '这个服务当前不需要升级',
          retryable: false,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/service-version-operations', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { operations: [] } }),
    })
  })

  await page.goto('/services')
  await page.getByRole('button', { name: '升级到 0.22.1' }).click()

  await expect(page.getByText('这个服务当前已是最新版本，无需升级')).toBeVisible()
  await expect(page.getByText('请检查填写的内容')).toHaveCount(0)
})

test('lists download files and manages uploads, deletion, and the release index', async ({
  page,
}) => {
  await useAdminSession(page)

  const indexedFile = {
    id: 'file-client',
    file_name: 'sprout-parent-app-0.12.3.apk',
    relative_path: '0.12.3/stable/android/apk/sprout-parent-app-0.12.3.apk',
    directory: '0.12.3/stable/android/apk',
    size_bytes: 48234496,
    modified_at: '2026-10-04T02:00:00Z',
    sha256: 'a'.repeat(64),
    download_url:
      'https://download.example.test/0.12.3/stable/android/apk/sprout-parent-app-0.12.3.apk',
    release: {
      version: '0.12.3',
      channel: 'stable',
      kind: 'apk',
      platform: 'android',
    },
    is_indexed: true,
  }
  const pendingFile = {
    id: 'file-firmware',
    file_name: 'sprout-firmware-0.12.3.bin',
    relative_path: '0.12.3/stable/esp32_s3/firmware/sprout-firmware-0.12.3.bin',
    directory: '0.12.3/stable/esp32_s3/firmware',
    size_bytes: 2097152,
    modified_at: '2026-10-04T02:10:00Z',
    sha256: 'b'.repeat(64),
    download_url:
      'https://download.example.test/0.12.3/stable/esp32_s3/firmware/sprout-firmware-0.12.3.bin',
    release: {
      version: '0.12.3',
      channel: 'stable',
      kind: 'firmware',
      platform: 'esp32_s3',
    },
    is_indexed: false,
  }
  let releaseFiles = [indexedFile, pendingFile]
  const uploadPayloads: Record<string, string> = {}
  const releasePayloads: string[] = []
  let refreshRequests = 0
  const deletedPaths: string[] = []

  await page.route('**/api/v1/admin/storage/files', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { files: releaseFiles } }),
    })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    const request = route.request()
    expect(request.method()).toBe('POST')
    const body = request.postData() ?? ''
    for (const field of [
      'name="file"',
      'name="relative_path"',
      'name="directory"',
      'name="filename"',
      'name="version"',
      'name="kind"',
      'name="platform"',
      'name="overwrite"',
    ]) {
      expect(body).toContain(field)
    }
    uploadPayloads.body = body
    const uploadedFile = {
      id: 'file-resource',
      file_name: 'learning-pack-0.12.3.zip',
      relative_path: '0.12.3/stable/all/resource/learning-pack-0.12.3.zip',
      directory: '0.12.3/stable/all/resource',
      size_bytes: 1024,
      modified_at: '2026-10-04T03:00:00Z',
      sha256: 'c'.repeat(64),
      download_url:
        'https://download.example.test/0.12.3/stable/all/resource/learning-pack-0.12.3.zip',
      release: {
        version: '0.12.3',
        channel: 'stable',
        kind: 'resource',
        platform: 'all',
      },
      is_indexed: true,
    }
    releaseFiles = [...releaseFiles, uploadedFile]
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { file: uploadedFile } }),
    })
  })
  await page.route('**/api/v1/admin/releases', async (route) => {
    const request = route.request()
    if (request.method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data: { releases: [] } }),
      })
      return
    }
    if (request.method() === 'POST') {
      releasePayloads.push(request.postData() ?? '')
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data: { release: {} } }),
      })
      return
    }
    await route.fallback()
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          available: true,
          refreshed_at: '2026-10-04T02:30:00Z',
          indexed_file_count: 1,
          pending_file_count: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/storage/index/refresh', async (route) => {
    refreshRequests += 1
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          available: true,
          refreshed_at: '2026-10-04T03:05:00Z',
          indexed_file_count: 2,
          pending_file_count: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/storage/files/**', async (route) => {
    const request = route.request()
    if (request.method() !== 'DELETE') {
      await route.fallback()
      return
    }
    const relativePath = decodeURIComponent(
      new URL(request.url()).pathname.replace('/api/v1/admin/storage/files/', ''),
    )
    deletedPaths.push(relativePath)
    releaseFiles = releaseFiles.filter((file) => file.relative_path !== relativePath)
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    })
  })

  await page.goto('/downloads')

  await expect(page.getByRole('heading', { name: '下载文件' })).toBeVisible()
  await expect(page.locator('table tbody tr')).toHaveCount(2)
  await expect(
    page.locator('table tbody').getByText('sprout-parent-app-0.12.3.apk', { exact: true }),
  ).toBeVisible()
  await expect(page.getByText('待登记', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('按版本查看')).toBeVisible()
  await expect(page.getByText('1 / 2 已登记')).toBeVisible()

  await page.getByLabel('发布状态').selectOption('pending')
  await expect(page.locator('table tbody tr')).toHaveCount(1)
  await expect(
    page.locator('table tbody').getByText('sprout-firmware-0.12.3.bin', { exact: true }),
  ).toBeVisible()

  await page.getByLabel('文件类型').selectOption('client')
  await expect(page.locator('table tbody tr')).toHaveCount(0)

  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(page.locator('table tbody tr')).toHaveCount(2)

  await page.getByRole('button', { name: '上传文件' }).click()
  const uploadDialog = page.getByRole('dialog', { name: '上传到下载服务器' })
  await expect(uploadDialog).toBeVisible()
  await uploadDialog.getByLabel('本地文件').setInputFiles({
    name: 'learning-pack-0.12.3.zip',
    mimeType: 'application/zip',
    buffer: Buffer.from('learning-pack'),
  })
  await uploadDialog.getByLabel('文件名').fill('learning-pack-0.12.3.zip')
  await uploadDialog.getByLabel('版本号').fill('0.12.3')
  await uploadDialog.getByLabel('文件类型').selectOption('resource')
  await uploadDialog.getByLabel('适用平台').selectOption('all')
  await uploadDialog.getByLabel('更新说明').fill('新增离线故事与发音练习。')
  await expect(uploadDialog.getByLabel('目标目录')).toHaveValue('0.12.3/stable/all/resource')
  await uploadDialog.getByRole('button', { name: '上传并登记版本' }).click()

  await expect(
    page.getByText('learning-pack-0.12.3.zip 已上传，并登记为待发布版本。'),
  ).toBeVisible()
  expect(uploadPayloads.body).toContain('0.12.3/stable/all/resource/learning-pack-0.12.3.zip')
  expect(releasePayloads).toHaveLength(1)
  expect(releasePayloads[0]).toContain('"kind":"resource"')
  expect(releasePayloads[0]).toContain('"platform":"all"')
  expect(releasePayloads[0]).toContain('"release_notes":"新增离线故事与发音练习。"')
  expect(refreshRequests).toBeGreaterThan(0)
  await expect(page.locator('table tbody tr')).toHaveCount(3)

  await page.getByRole('button', { name: '删除' }).first().click()
  const deleteDialog = page.getByRole('dialog', { name: /删除 .*？/ })
  await expect(deleteDialog).toBeVisible()
  await deleteDialog.getByRole('button', { name: '确认删除' }).click()
  await expect(page.getByText('已从下载服务器删除。')).toBeVisible()
  expect(deletedPaths[0]).toBe('0.12.3/stable/android/apk/sprout-parent-app-0.12.3.apk')
})

test('shows complete device diagnostics with bounded history and refresh', async ({ page }) => {
  await useAdminSession(page)

  const devices = [
    {
      device_id: 'device-1',
      device_name: '初芽一号',
      hardware_model: '初芽标准版',
      firmware_version: '0.12.3',
      lifecycle_status: 'active',
      runtime: {
        is_online: true,
        connection: { state: 'connected', transport: 'wifi' },
        network_quality: {
          level: 'poor',
          rssi_dbm: -88,
          latency_ms: 1200,
          packet_loss_percent: 15,
        },
      },
    },
    {
      device_id: 'device-2',
      device_name: '初芽二号',
      hardware_model: '初芽标准版',
      firmware_version: '0.12.2',
      lifecycle_status: 'active',
      runtime: {
        is_online: false,
        connection: { state: 'disconnected', transport: 'wifi' },
      },
    },
  ]
  const diagnosticsByDevice: Record<string, unknown> = {
    'device-1': {
      device_id: 'device-1',
      boot_events: [
        {
          event_id: 'boot_00000003',
          event_type: 'boot',
          sequence: 3,
          uptime_ms: 1200,
          boot_count: 3,
          reset_reason: 'power_on',
          firmware_version: '0.12.3',
          reported_at: '2026-10-04T03:00:00Z',
        },
      ],
      failures: [
        {
          event_id: 'failure_00000004',
          event_type: 'module_failure',
          sequence: 4,
          module_name: 'network_manager',
          error_code: 'ESP_ERR_TIMEOUT',
          failure_count: 2,
          firmware_version: '0.12.3',
          reported_at: '2026-10-04T03:01:00Z',
        },
      ],
      recovery_events: [
        {
          event_id: 'recovery_00000005',
          event_type: 'module_recovered',
          sequence: 5,
          module_name: 'network_manager',
          firmware_version: '0.12.3',
          reported_at: '2026-10-04T03:02:00Z',
        },
      ],
      latest_failure: {
        event_id: 'failure_00000004',
        event_type: 'module_failure',
        sequence: 4,
        module_name: 'network_manager',
        error_code: 'ESP_ERR_TIMEOUT',
        failure_count: 2,
        firmware_version: '0.12.3',
        reported_at: '2026-10-04T03:01:00Z',
      },
      error_count: 2,
      recovery_count: 1,
      updated_at: '2026-10-04T03:02:00Z',
      health_state: 'degraded',
      retention_boot_events: 5,
      retention_failures: 10,
      retention_recovery_events: 10,
      crash_events: [
        {
          event_id: 'crash_00000006',
          event_type: 'crash',
          sequence: 6,
          cause: 'task_watchdog',
          fault_kind: 'watchdog',
          task_name: 'network_manager',
          program_counter: 1074590260,
          frame_count: 12,
          core_id: 0,
          firmware_version: '0.12.3',
          reported_at: '2026-10-04T00:00:00Z',
        },
      ],
      crash_count: 1,
      retention_crash_events: 50,
    },
    'device-2': {
      device_id: 'device-2',
      boot_events: [],
      failures: [],
      recovery_events: [],
      latest_failure: null,
      error_count: 0,
      recovery_count: 0,
      updated_at: '2026-10-04T02:00:00Z',
      health_state: 'healthy',
      retention_boot_events: 5,
      retention_failures: 10,
      retention_recovery_events: 10,
      crash_events: [],
      crash_count: 0,
      retention_crash_events: 50,
    },
  }
  const requestedDeviceIds: string[] = []

  const summariesByDevice: Record<string, unknown> = {
    'device-1': {
      device_id: 'device-1',
      window_days: 7,
      boot_count: 5,
      failure_count: 10,
      recovery_count: 10,
      crash_count: 3,
      crash_causes: [
        { cause: 'task_watchdog', count: 2 },
        { cause: 'brownout', count: 1 },
      ],
      health_state: 'faulted',
      last_reported_at: '2026-10-04T00:00:00Z',
      generated_at: '2026-10-04T00:00:00Z',
    },
    'device-2': {
      device_id: 'device-2',
      window_days: 7,
      boot_count: 0,
      failure_count: 0,
      recovery_count: 0,
      crash_count: 0,
      crash_causes: [],
      health_state: 'healthy',
      last_reported_at: null,
      generated_at: '2026-10-04T00:00:00Z',
    },
  }

  await page.route('**/api/v1/admin/devices', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { devices } }),
    })
  })
  await page.route('**/api/v1/admin/devices/*/diagnostics**', async (route) => {
    const requestUrl = new URL(route.request().url())
    const segments = requestUrl.pathname.split('/').filter(Boolean)
    if (segments.at(-1) === 'summary') {
      const deviceId = segments.at(-3) ?? ''
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ data: summariesByDevice[deviceId] }),
      })
      return
    }
    const deviceId = segments.at(-2) ?? ''
    requestedDeviceIds.push(deviceId)
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: diagnosticsByDevice[deviceId] }),
    })
  })

  await page.goto('/device-diagnostics')

  await expect(page.getByRole('heading', { name: '设备诊断' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '选择设备' })).toBeVisible()
  await expect(page.getByRole('button', { name: /初芽一号/ })).toBeVisible()
  expect(requestedDeviceIds).toContain('device-1')

  await expect(page.getByText('需要留意', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('网络较差')).toBeVisible()
  await expect(page.getByRole('heading', { name: '启动历史' })).toBeVisible()
  await expect(page.getByText('开机启动')).toBeVisible()
  await expect(page.getByRole('heading', { name: '最近故障' })).toBeVisible()
  await expect(page.getByText('网络连接出现异常')).toBeVisible()
  await expect(page.getByText(/连接等待超时/)).toBeVisible()
  await expect(page.getByRole('heading', { name: '恢复事件' })).toBeVisible()
  await expect(page.getByText('网络连接已恢复')).toBeVisible()
  await expect(page.getByRole('heading', { name: '异常重启' })).toBeVisible()
  await expect(page.getByText('任务守护超时').first()).toBeVisible()
  await expect(page.getByRole('heading', { name: '记录保留说明' })).toBeVisible()
  await expect(page.getByText(/启动记录 5 条/)).toBeVisible()
  await expect(page.getByText(/故障记录 10 条/)).toBeVisible()
  await expect(page.getByText(/异常重启记录 50 条/)).toBeVisible()
  await expect(page.getByRole('heading', { name: '近 7 天趋势' })).toBeVisible()
  await expect(page.getByText('异常重启原因分布')).toBeVisible()

  await page.getByRole('button', { name: /初芽二号/ }).click()
  await expect(page.getByText('运行正常', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('最近没有故障记录，设备运行状态良好。')).toBeVisible()
  await expect(page.getByText('还没有恢复记录。设备恢复正常后会自动显示。')).toBeVisible()
  await expect(page.getByText('最近没有异常重启记录，设备启动都很平稳。')).toBeVisible()
  await expect(page.getByText('窗口内没有异常重启，设备运行平稳。')).toBeVisible()
  expect(requestedDeviceIds.filter((deviceId) => deviceId === 'device-2')).toHaveLength(1)

  await page.getByRole('button', { name: '刷新诊断' }).click()
  await expect(page.getByRole('button', { name: '刷新诊断' })).toBeEnabled()
  expect(requestedDeviceIds.filter((deviceId) => deviceId === 'device-2')).toHaveLength(2)
})

test('recovers the device diagnostics page from list and detail failures', async ({ page }) => {
  await useAdminSession(page)

  const device = {
    device_id: 'device-recovery',
    device_name: '初芽恢复测试',
    hardware_model: '初芽标准版',
    firmware_version: '0.12.3',
    lifecycle_status: 'active',
    runtime: {
      is_online: true,
      connection: { state: 'connected', transport: 'wifi' },
    },
  }
  let deviceRequests = 0
  let diagnosticsRequests = 0

  await page.route('**/api/v1/admin/devices', async (route) => {
    deviceRequests += 1
    if (deviceRequests === 1) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'temporarily_unavailable', message: '服务暂时不可用' },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { devices: [device] } }),
    })
  })
  await page.route('**/api/v1/admin/devices/device-recovery/diagnostics', async (route) => {
    diagnosticsRequests += 1
    if (diagnosticsRequests === 1) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'temporarily_unavailable', message: '服务暂时不可用' },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          device_id: 'device-recovery',
          boot_events: [],
          failures: [],
          recovery_events: [],
          latest_failure: null,
          error_count: 0,
          recovery_count: 0,
          updated_at: '2026-10-04T03:10:00Z',
          health_state: 'healthy',
          retention_boot_events: 5,
          retention_failures: 10,
          retention_recovery_events: 10,
        },
      }),
    })
  })

  await page.goto('/device-diagnostics')

  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await page.getByRole('button', { name: '重新加载设备' }).click()
  await expect(page.getByRole('button', { name: /初芽恢复测试/ })).toBeVisible()
  expect(deviceRequests).toBe(2)

  await expect(page.getByText('正在读取设备健康信息…')).toBeHidden()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await page.getByRole('button', { name: '重新加载' }).click()
  await expect(page.getByText('运行正常', { exact: true }).first()).toBeVisible()
  expect(diagnosticsRequests).toBe(2)
})

test('manages the content library lifecycle from draft to published', async ({ page }) => {
  await useAdminSession(page)

  // The list endpoint returns one row per package version, mirroring the Go
  // domain so the client can fold versions into a package.
  const storyVersion = () => ({
    package_id: 'content_story_001',
    package_version: 1,
    title: '小兔子的一天',
    category: 'story',
    age_tiers: ['age_3_4'],
    asset_key: '1.0.0/stable/all/resource/story-001.zip',
    sha256: 'a'.repeat(64),
    size_bytes: 2048,
    status: 'draft',
    created_at: '2026-10-05T02:00:00Z',
    updated_at: '2026-10-05T02:00:00Z',
  })
  let rows = [storyVersion()]
  const history: Record<string, unknown>[] = []
  let createPayload = ''
  const rejectPayloads: string[] = []
  const downloads = [
    {
      file_name: 'story-001.zip',
      relative_path: '1.0.0/stable/all/resource/story-001.zip',
      directory: '1.0.0/stable/all/resource',
      size_bytes: 2048,
      sha256: 'a'.repeat(64),
      download_url: 'https://download.example.test/1.0.0/stable/all/resource/story-001.zip',
      is_indexed: true,
    },
  ]

  await page.route('**/api/v1/admin/content/packages**', async (route) => {
    const request = route.request()
    const requestUrl = new URL(request.url())
    const path = requestUrl.pathname

    if (path === '/api/v1/admin/content/packages' && request.method() === 'GET') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: { packages: rows, page: 1, page_size: 20, total: rows.length },
        }),
      })
      return
    }
    if (path === '/api/v1/admin/content/packages' && request.method() === 'POST') {
      createPayload = request.postData() ?? ''
      const created = {
        package_id: 'content_story_002',
        package_version: 1,
        title: '小熊的清晨',
        category: 'story',
        age_tiers: ['age_5_6'],
        asset_key: '1.0.0/stable/all/resource/story-001.zip',
        sha256: 'a'.repeat(64),
        size_bytes: 2048,
        status: 'draft',
        created_at: '2026-10-05T03:00:00Z',
        updated_at: '2026-10-05T03:00:00Z',
      }
      rows = [...rows, created]
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({ data: { package: created } }),
      })
      return
    }

    // Detail is the only read for a single package; actions return one version.
    const segments = path.split('/')
    const versionIndex = segments.indexOf('versions')
    if (versionIndex === -1) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            package_id: 'content_story_001',
            versions: rows.filter((row) => row.package_id === 'content_story_001'),
            history,
          },
        }),
      })
      return
    }
    const action = segments[segments.length - 1]
    const current = rows[0]
    if (action === 'reject') {
      rejectPayloads.push(request.postData() ?? '')
      current.status = 'draft'
      history.push({
        package_version: 1,
        action: 'reject',
        reason: '音频有杂音，请重新录制。',
        actor_account_id: 'test-admin',
        created_at: '2026-10-05T03:30:00Z',
      })
    } else if (action === 'submit') {
      current.status = 'in_review'
      history.push({
        package_version: 1,
        action: 'submit',
        reason: '',
        actor_account_id: 'test-admin',
        created_at: '2026-10-05T03:40:00Z',
      })
    } else if (action === 'publish') {
      current.status = 'published'
      history.push({
        package_version: 1,
        action: 'publish',
        reason: '',
        actor_account_id: 'test-admin',
        created_at: '2026-10-05T03:50:00Z',
      })
    }
    rows = [current, ...rows.slice(1)]
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { package: current } }),
    })
  })

  await page.route('**/api/v1/admin/storage/files**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { files: downloads } }),
    })
  })

  await page.goto('/content')

  await expect(page.getByRole('heading', { name: '内容库' })).toBeVisible()
  await expect(page.locator('table tbody tr')).toHaveCount(1)
  await expect(page.getByText('小兔子的一天', { exact: true })).toBeVisible()
  await expect(page.locator('table tbody tr').getByText('草稿', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '新建内容' }).click()
  const createDialog = page.getByRole('dialog', { name: '新建内容草稿' })
  await expect(createDialog).toBeVisible()
  await createDialog.getByLabel('内容编号').fill('content_story_002')
  await createDialog.getByLabel('标题').fill('小熊的清晨')
  await createDialog.getByText('5-6 岁').click()
  await createDialog.getByRole('button', { name: '选择下载文件' }).click()
  await createDialog
    .getByLabel('选择已经上传的文件')
    .selectOption('1.0.0/stable/all/resource/story-001.zip')
  await expect(createDialog.getByLabel('校验值')).toHaveValue('a'.repeat(64))
  await expect(createDialog.getByLabel('文件大小（字节）')).toHaveValue('2048')
  await createDialog.getByLabel('文件大小（字节）').fill('786433')
  await createDialog.getByRole('button', { name: '创建草稿' }).click()
  await expect(createDialog.getByText('文件大小需要在 1 字节到 768 KiB 之间。')).toBeVisible()
  expect(createPayload).toBe('')
  await createDialog.getByLabel('文件大小（字节）').fill('2048')
  await createDialog.getByRole('button', { name: '创建草稿' }).click()

  await expect(page.getByText('内容草稿已创建。')).toBeVisible()
  expect(createPayload).toContain('"package_id":"content_story_002"')
  expect(createPayload).toContain('"category":"story"')
  expect(createPayload).toContain('"age_tiers":["age_5_6"]')
  expect(createPayload).toContain(`"sha256":"${'a'.repeat(64)}"`)

  await expect(page.locator('table tbody tr')).toHaveCount(2)
  await page.getByRole('button', { name: '管理' }).first().click()
  const drawer = page.getByRole('dialog', { name: '小兔子的一天' })
  await expect(drawer).toBeVisible()
  await expect(page.getByRole('heading', { name: '第 1 版' })).toBeVisible()
  await expect(page.getByText('还没有审核记录。')).toBeVisible()

  await page.getByRole('button', { name: '提交审核' }).click()
  await expect(page.getByText('已提交审核。')).toBeVisible()

  await page.getByRole('button', { name: '审核驳回' }).click()
  const rejectDialog = page.getByRole('dialog', { name: '驳回第 1 版？' })
  await expect(rejectDialog).toBeVisible()
  await rejectDialog.getByRole('button', { name: '确认驳回' }).click()
  await expect(rejectDialog.getByText('请填写驳回理由，便于作者修改后再提交。')).toBeVisible()
  expect(rejectPayloads).toHaveLength(0)
  await rejectDialog.getByLabel('驳回理由').fill('音频有杂音，请重新录制。')
  await rejectDialog.getByRole('button', { name: '确认驳回' }).click()
  await expect(page.getByText('已驳回该版本。')).toBeVisible()
  expect(rejectPayloads[0]).toContain('音频有杂音，请重新录制。')

  await page.getByRole('button', { name: '提交审核' }).click()
  await expect(page.getByText('已提交审核。')).toBeVisible()
  await expect(
    page.locator('table tbody tr').getByText('待审核', { exact: true }).first(),
  ).toBeVisible()

  await page.getByRole('button', { name: '发布' }).click()
  await expect(page.getByText('内容已发布。')).toBeVisible()
  await expect(
    page.locator('table tbody tr').getByText('已发布', { exact: true }).first(),
  ).toBeVisible()
})

test('shows family usage reports and recovers from a report read failure', async ({ page }) => {
  await useAdminSession(page)

  let reportRequests = 0
  const requestedDays: string[] = []
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ data: { models: [] } }),
    })
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-usage-1',
              display_name: '小芽家长',
              phone: '13800001111',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-usage-1/usage-reports**', async (route) => {
    reportRequests += 1
    const requestUrl = new URL(route.request().url())
    requestedDays.push(requestUrl.searchParams.get('days') ?? '')
    if (reportRequests === 1) {
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({
          error: { code: 'temporarily_unavailable', message: '服务暂时不可用' },
        }),
      })
      return
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          reports: [
            {
              schema_version: '1.0.0',
              report_date: '2026-10-07',
              timezone_offset_minutes: 480,
              has_policy: true,
              has_daily_limit: true,
              active_minutes: 42,
              conversation_count: 18,
              conversation_minutes: 24,
              content_play_count: 6,
              content_minutes: 18,
              daily_limit_minutes: 60,
              remaining_minutes: 18,
              limit_reached: false,
              categories: [
                { category: 'story', play_count: 3, minutes: 9 },
                { category: 'nursery_rhyme', play_count: 2, minutes: 6 },
              ],
              blocked: {
                disabled_period: 1,
                daily_limit: 0,
                category_denied: 2,
                time_untrusted: 0,
              },
              devices: [
                {
                  device_id: 'device-usage-1',
                  device_name: '初芽一号',
                  active_minutes: 42,
                  conversation_count: 18,
                  content_play_count: 6,
                },
              ],
              updated_at: '2026-10-07T12:00:00Z',
            },
            {
              schema_version: '1.0.0',
              report_date: '2026-10-06',
              timezone_offset_minutes: 480,
              has_policy: true,
              has_daily_limit: true,
              active_minutes: 12,
              conversation_count: 4,
              conversation_minutes: 5,
              content_play_count: 2,
              content_minutes: 7,
              daily_limit_minutes: 60,
              remaining_minutes: 48,
              limit_reached: false,
              categories: [{ category: 'bedtime', play_count: 1, minutes: 3 }],
              blocked: {
                disabled_period: 0,
                daily_limit: 1,
                category_denied: 0,
                time_untrusted: 0,
              },
              devices: [
                {
                  device_id: 'device-usage-1',
                  device_name: '初芽一号',
                  active_minutes: 12,
                  conversation_count: 4,
                  content_play_count: 2,
                },
              ],
              updated_at: '2026-10-06T12:00:00Z',
            },
          ],
        },
      }),
    })
  })

  await page.goto('/usage-reports')

  await expect(page.getByRole('heading', { name: '使用报告' })).toBeVisible()
  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await page.getByRole('button', { name: '重新加载' }).click()

  await expect(page.getByRole('heading', { name: '小芽家长' })).toBeVisible()
  await expect(page.getByText('54', { exact: true })).toBeVisible()
  await expect(page.getByText('22', { exact: true })).toBeVisible()
  await expect(page.getByText('8', { exact: true })).toBeVisible()
  await expect(page.getByText('4', { exact: true }).last()).toBeVisible()
  await expect(page.getByText('2026-10-07')).toBeVisible()
  await expect(page.getByText('故事')).toBeVisible()
  await expect(page.getByText('免打扰时段')).toBeVisible()
  await expect(page.getByText('初芽一号', { exact: true }).first()).toBeVisible()

  await page.getByRole('button', { name: '最近 30 天' }).click()
  await expect(page.getByRole('button', { name: '最近 30 天' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  expect(requestedDays).toEqual(['7', '7', '30'])
})

test('distinguishes a missing guardian policy from a policy read failure', async ({ page }) => {
  await useAdminSession(page)

  await page.route('**/api/v1/admin/families**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          accounts: [
            {
              parent_account_id: 'parent-policy-1',
              display_name: '政策测试家长',
              phone: '13900002222',
            },
          ],
          total: 1,
          page: 1,
          page_size: 20,
          total_pages: 1,
        },
      }),
    })
  })
  await page.route('**/api/v1/admin/families/parent-policy-1/children', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          children: [
            {
              child_id: 'child-policy-1',
              family_id: 'parent-policy-1',
              nickname: '小满',
              age_tier: 'age_5_6',
              interests: [],
              content_categories: ['story'],
              guardian_consent: true,
              created_at: '2026-10-01T00:00:00Z',
              updated_at: '2026-10-07T00:00:00Z',
              policy: null,
            },
            {
              child_id: 'child-policy-2',
              family_id: 'parent-policy-1',
              nickname: '小禾',
              age_tier: 'age_3_4',
              interests: [],
              content_categories: ['story'],
              guardian_consent: true,
              created_at: '2026-10-01T00:00:00Z',
              updated_at: '2026-10-07T00:00:00Z',
              policy_state: 'read_failed',
              policy_error: '策略暂时无法读取，请稍后重试',
            },
          ],
        },
      }),
    })
  })

  await page.goto('/children')

  await expect(page.getByRole('heading', { name: '儿童档案' })).toBeVisible()
  await page.getByRole('button', { name: /小满/ }).click()
  await expect(
    page.getByText('监护人尚未设置时间与内容策略。监护人保存后，这里会显示最新规则。'),
  ).toBeVisible()

  await page.getByRole('button', { name: /小禾/ }).click()
  await expect(page.getByText('策略读取失败')).toBeVisible()
  await expect(page.getByText('策略暂时无法读取，请稍后重试')).toBeVisible()
  await expect(page.getByRole('button', { name: '重新读取' })).toBeVisible()
})
