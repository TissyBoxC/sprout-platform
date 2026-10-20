import { expect, test, type Page, type Route } from '@playwright/test'

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
            admin_permissions: [
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
              'settings.read',
              'settings.manage',
              'notifications.read',
              'notifications.manage',
              'stats.read',
            ],
          },
        },
      }),
    })
  })
}

async function fulfillJSON(
  route: Route,
  status: number,
  data: unknown,
): Promise<void> {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(data),
  })
}

function familyPayload(
  parentAccountId: string,
  displayName: string,
): Record<string, unknown> {
  return {
    parent_account_id: parentAccountId,
    display_name: displayName,
    phone: '13800000000',
    guardian_family_name: displayName,
    child_nickname: '小芽',
    child_birthday: '2021-06-01',
    status: 'active',
    ai_account: {
      provider_account_id: `provider-${parentAccountId}`,
      status: 'active',
      balance_usd: 20,
      concurrency_limit: 2,
      available_models: ['model-a'],
      selected_models: ['model-a'],
      allowed_models: ['model-a'],
      credential_ready: true,
    },
  }
}

function familyListPayload(
  accounts: Record<string, unknown>[],
  page = 1,
  pageSize = 20,
  total = accounts.length,
): Record<string, unknown> {
  return {
    data: {
      accounts,
      pagination: {
        page,
        page_size: pageSize,
        total,
        total_pages: total === 0 ? 0 : Math.ceil(total / pageSize),
        has_more: page * pageSize < total,
      },
    },
  }
}

function notification(id: string, title: string): Record<string, unknown> {
  return {
    id,
    category: 'system_announcement',
    severity: 'info',
    title,
    body: `${title}的说明`,
    action_path: '',
    action_label: '',
    audience: 'all_parents',
    parent_account_id: '',
    device_id: '',
    display_duration_seconds: 0,
    channels: ['in_app'],
    source: 'administrator',
    created_by: 'test-admin',
    publish_at: '2026-10-18T09:00:00Z',
    expires_at: '',
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
    delivery_total: 1,
    delivery_pending: 0,
    delivery_delivered: 1,
    delivery_read: 0,
    delivery_failed: 0,
  }
}

function usageReport(
  reportDate: string,
  activeMinutes: number,
): Record<string, unknown> {
  return {
    schema_version: '1.0.0',
    report_date: reportDate,
    timezone_offset_minutes: 480,
    has_policy: true,
    has_daily_limit: true,
    active_minutes: activeMinutes,
    conversation_count: activeMinutes,
    conversation_minutes: activeMinutes,
    content_play_count: 0,
    content_minutes: 0,
    daily_limit_minutes: 60,
    remaining_minutes: 60 - activeMinutes,
    limit_reached: false,
    categories: [],
    blocked: {
      disabled_period: 0,
      daily_limit: 0,
      category_denied: 0,
      time_untrusted: 0,
    },
    devices: [],
    updated_at: '2026-10-18T09:00:00Z',
  }
}

function releasePayload(version: string): Record<string, unknown> {
  return {
    id: `release-${version}`,
    version,
    channel: 'stable',
    kind: 'client',
    platform: 'android',
    download_url: `https://download.example.test/${version}.apk`,
    sha256: 'a'.repeat(64),
    release_notes: `${version} 更新说明`,
    is_mandatory: false,
    min_supported_version: '',
    status: 'published',
    published_at: '2026-10-18T09:00:00Z',
    created_at: '2026-10-18T09:00:00Z',
  }
}

function releaseArtifactPayload(
  version: string,
  kind: string,
  platform: string,
  channel: string,
): Record<string, unknown> {
  return {
    id: `release-${version}-${channel}-${kind}-${platform}`,
    version,
    channel,
    kind,
    platform,
    download_url: `https://download.example.test/${version}/${channel}/${platform}/${kind}.bin`,
    sha256: 'b'.repeat(64),
    release_notes: `${version} ${kind}`,
    is_mandatory: false,
    min_supported_version: '',
    status: 'draft',
    published_at: '',
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
  }
}

function childPayload(nickname: string): Record<string, unknown> {
  return {
    child_id: `child-${nickname}`,
    family_id: `family-${nickname}`,
    nickname,
    age_tier: 'age_5_6',
    interests: ['stories'],
    content_categories: ['story'],
    guardian_consent: true,
    created_at: '2026-10-18T09:00:00Z',
    updated_at: '2026-10-18T09:00:00Z',
    policy_state: 'not_set',
  }
}

function uploadDialog(page: Page) {
  return page.getByRole('dialog', { name: '上传到下载服务器' })
}

async function fillUploadForm(
  page: Page,
  options: {
    filename: string
    version: string
    kind: string
    platform: string
    notes: string
  },
): Promise<ReturnType<typeof uploadDialog>> {
  await page.getByRole('button', { name: '上传文件' }).click()
  const dialog = uploadDialog(page)
  await dialog.getByLabel('本地文件').setInputFiles({
    name: options.filename,
    mimeType: 'application/octet-stream',
    buffer: Buffer.from(options.filename),
  })
  await dialog.getByLabel('文件名').fill(options.filename)
  await dialog.getByLabel('版本号').fill(options.version)
  await dialog.getByLabel('更新渠道').selectOption('stable')
  await dialog.getByLabel('文件类型').selectOption(options.kind)
  await dialog.getByLabel('适用平台').selectOption(options.platform)
  await dialog.getByLabel('更新说明').fill(options.notes)
  return dialog
}

function uploadedFile(
  filename: string,
  version: string,
  kind: string,
  platform: string,
): Record<string, unknown> {
  const directoryKind =
    kind === 'client' && platform === 'android' && filename.endsWith('.apk')
      ? 'apk'
      : kind
  return {
    id: `uploaded-${version}`,
    name: filename,
    file_name: filename,
    relative_path: `${version}/stable/${platform}/${directoryKind}/${filename}`,
    directory: `${version}/stable/${platform}/${directoryKind}`,
    size_bytes: 2048,
    modified_at: '2026-10-18T09:00:00Z',
    sha256: 'b'.repeat(64),
    download_url: `https://download.example.test/${version}/stable/${platform}/${directoryKind}/${filename}`,
    version,
    kind,
    platform,
    is_indexed: false,
  }
}

function indexStatusPayload(): Record<string, unknown> {
  return {
    data: {
      available: true,
      refreshed_at: '2026-10-18T09:00:00Z',
      indexed_file_count: 0,
      pending_file_count: 1,
    },
  }
}

test('keeps the newest notification filter response and exposes filter recovery', async ({
  page,
}) => {
  await useAdminSession(page)
  let releaseOldRequest: (() => Promise<void>) | null = null
  await page.route('**/api/v1/admin/notifications**', async (route) => {
    const requestURL = new URL(route.request().url())
    if (requestURL.pathname.endsWith('/stats')) {
      await fulfillJSON(route, 200, {
        data: {
          total: 0,
          broadcast_count: 0,
          pending_delivery_count: 0,
          failed_delivery_count: 0,
        },
      })
      return
    }
    const query = requestURL.searchParams.get('query') ?? ''
    if (query === '旧') {
      await new Promise<void>((resolve) => {
        releaseOldRequest = async () => {
          await fulfillJSON(route, 200, {
            data: {
              items: [notification('old', '旧筛选结果')],
              total: 1,
              total_pages: 1,
            },
          })
          resolve()
        }
      })
      return
    }
    if (query === '新') {
      const category = requestURL.searchParams.get('category') ?? ''
      if (category === 'content_release') {
        await fulfillJSON(route, 200, {
          data: {
            items: [],
            total: 0,
            total_pages: 1,
          },
        })
        return
      }
      await fulfillJSON(route, 200, {
        data: {
          items: [notification('new', '新筛选结果')],
          total: 1,
          total_pages: 1,
        },
      })
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        items: [],
        total: 0,
        total_pages: 1,
      },
    })
  })

  await page.goto('/notifications')
  await expect(page.getByText('还没有发布任何通知')).toBeVisible()

  await page.getByPlaceholder('搜索标题或内容').fill('旧')
  await expect.poll(() => releaseOldRequest !== null).toBe(true)
  await page.getByPlaceholder('搜索标题或内容').fill('新')
  await expect(page.getByText('新筛选结果', { exact: true })).toBeVisible()
  await (releaseOldRequest as unknown as () => Promise<void>)()
  await expect(page.getByText('新筛选结果', { exact: true })).toBeVisible()
  await expect(page.getByText('旧筛选结果', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '上一页' })).toBeDisabled()

  await page.getByLabel('按类型筛选').selectOption('content_release')
  await page.waitForTimeout(350)
  await expect(page.getByText('没有符合当前条件的通知')).toBeVisible()
  await expect(page.getByRole('button', { name: '清除筛选' })).toBeVisible()
})

test('does not start overlapping notification page loads and preserves a valid empty page', async ({
  page,
}) => {
  await useAdminSession(page)
  const releasePageLoad = Promise.withResolvers<void>()
  const pageRequests: string[] = []
  await page.route('**/api/v1/admin/notifications**', async (route) => {
    const requestURL = new URL(route.request().url())
    if (requestURL.pathname.endsWith('/stats')) {
      await fulfillJSON(route, 200, {
        data: {
          total: 21,
          broadcast_count: 0,
          pending_delivery_count: 0,
          failed_delivery_count: 0,
        },
      })
      return
    }
    const requestedPage = Number(requestURL.searchParams.get('page') ?? '1')
    pageRequests.push(String(requestedPage))
    if (requestedPage === 2) {
      await releasePageLoad.promise
      await fulfillJSON(route, 200, {
        data: {
          items: [],
          total: 21,
          total_pages: 2,
        },
      })
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        items: [notification('page-1', '第一页通知')],
        total: 21,
        total_pages: 2,
      },
    })
  })

  await page.goto('/notifications')
  await expect(page.getByText('第一页通知', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect.poll(() => pageRequests.filter((value) => value === '2').length).toBe(1)
  expect(pageRequests.filter((value) => value === '2')).toHaveLength(1)
  const nextPage = page.getByRole('button', { name: '下一页' })
  await expect(nextPage).toBeDisabled()
  await nextPage.dispatchEvent('click')
  await nextPage.dispatchEvent('click')
  expect(pageRequests.filter((value) => value === '2')).toHaveLength(1)
  releasePageLoad.resolve()
  await expect(page.getByText('这一页没有通知')).toBeVisible()
  await expect(page.getByRole('button', { name: '返回上一页' })).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByRole('button', { name: '下一个' })).toHaveCount(0)
})

test('ignores a late usage report response for the previous family', async ({ page }) => {
  await useAdminSession(page)
  const firstReportIntercepted = Promise.withResolvers<void>()
  const releaseFirstReport = Promise.withResolvers<void>()
  await page.route(/\/api\/v1\/admin\/families(?:\?.*)?$/, async (route) => {
    await fulfillJSON(
      route,
      200,
      familyListPayload([
        familyPayload('family-a', '甲家庭'),
        familyPayload('family-b', '乙家庭'),
      ]),
    )
  })
  await page.route(
    '**/api/v1/admin/families/family-a/usage-reports**',
    async (route) => {
      firstReportIntercepted.resolve()
      await releaseFirstReport.promise
      await fulfillJSON(route, 200, {
        data: { reports: [usageReport('2026-10-01', 11)] },
      })
    },
  )
  await page.route(
    '**/api/v1/admin/families/family-b/usage-reports**',
    async (route) => {
      await fulfillJSON(route, 200, {
        data: { reports: [usageReport('2026-10-02', 22)] },
      })
    },
  )

  await page.goto('/usage-reports')
  await firstReportIntercepted.promise
  await page
    .getByRole('button')
    .filter({ hasText: '乙家庭' })
    .click()
  await expect(page.getByText('22', { exact: true }).first()).toBeVisible()
  releaseFirstReport.resolve()
  await expect(page.getByText('22', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('11', { exact: true })).toHaveCount(0)
})

test('keeps the newest child reload for a family and clears loading only for the newest request', async ({
  page,
}) => {
  await useAdminSession(page)
  const firstReloadIntercepted = Promise.withResolvers<void>()
  const releaseFirstReload = Promise.withResolvers<void>()
  let childRequests = 0
  await page.route(/\/api\/v1\/admin\/families(?:\?.*)?$/, async (route) => {
    await fulfillJSON(
      route,
      200,
      familyListPayload([familyPayload('family-child-a', '甲家庭')]),
    )
  })
  await page.route(
    '**/api/v1/admin/families/family-child-a/children',
    async (route) => {
      childRequests += 1
      if (childRequests === 2) {
        firstReloadIntercepted.resolve()
        await releaseFirstReload.promise
        await fulfillJSON(route, 200, {
          data: { children: [childPayload('旧档案')] },
        })
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 120))
      await fulfillJSON(route, 200, {
        data: {
          children: [childPayload(childRequests === 1 ? '初始档案' : '新档案')],
        },
      })
    },
  )

  await page.goto('/children')
  await expect(page.getByText('初始档案', { exact: true })).toBeVisible()
  await page
    .getByRole('button', { name: /正在更新…|更新档案/ })
    .click()
  await firstReloadIntercepted.promise
  const loadingChildren = page.getByRole('button', { name: '正在更新…' })
  await expect(loadingChildren).toBeDisabled()
  expect(childRequests).toBe(2)
  releaseFirstReload.resolve()
  await expect(page.getByText('旧档案', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '更新档案' })).toBeEnabled()
})

test('keeps the old family page size after a failed page-size request', async ({
  page,
}) => {
  await useAdminSession(page)
  let pageSizeRequestCount = 0
  const requestedPages: number[] = []
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await fulfillJSON(route, 200, { data: { models: [] } })
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    const requestURL = new URL(route.request().url())
    const path = requestURL.pathname
    if (path.endsWith('/devices') || path.endsWith('/ai-usage')) {
      await fulfillJSON(route, 200, { data: {} })
      return
    }
    const pageNumber = Number(requestURL.searchParams.get('page') ?? '1')
    const pageSize = Number(requestURL.searchParams.get('page_size') ?? '20')
    requestedPages.push(pageNumber)
    if (pageSize === 50) {
      pageSizeRequestCount += 1
      await fulfillJSON(route, 503, {
        error: { code: 'service_error', message: '暂时不可用' },
      })
      return
    }
    if (pageNumber === 2) {
      await fulfillJSON(route, 200, {
        data: {
          accounts: [familyPayload('family-page-2', '第二页家庭')],
          pagination: {
            page: 2,
            page_size: pageSize,
            total: 21,
            total_pages: 2,
          },
        },
      })
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        accounts: [familyPayload('family-page-1', '第一页家庭')],
        pagination: {
          page: 1,
          page_size: pageSize,
          total: 21,
          total_pages: 2,
        },
      },
    })
  })

  await page.goto('/families')
  await expect(page.getByText('第一页家庭')).toBeVisible()
  const pageSizeSelect = page.locator('.pagination select')
  await pageSizeSelect.selectOption('50')
  await expect(pageSizeSelect).toHaveValue('20')
  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  expect(pageSizeRequestCount).toBe(1)

  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('第二页家庭')).toBeVisible()
  expect(requestedPages).toContain(2)
})

test('keeps family pagination and recovery controls when the current page is empty', async ({
  page,
}) => {
  await useAdminSession(page)
  let pageSizeRequestCount = 0
  await page.route('**/api/v1/admin/ai-models', async (route) => {
    await fulfillJSON(route, 200, { data: { models: [] } })
  })
  await page.route('**/api/v1/admin/families**', async (route) => {
    const requestURL = new URL(route.request().url())
    if (requestURL.pathname !== '/api/v1/admin/families') {
      await fulfillJSON(route, 200, { data: {} })
      return
    }
    const pageNumber = Number(requestURL.searchParams.get('page') ?? '1')
    const pageSize = Number(requestURL.searchParams.get('page_size') ?? '20')
    if (pageSize === 50) {
      pageSizeRequestCount += 1
    }
    await fulfillJSON(route, 200, {
      data: {
        accounts:
          pageNumber === 1
            ? [familyPayload('family-empty-1', '第一页家庭')]
            : [],
        pagination: {
          page: pageNumber,
          page_size: pageSize,
          total: 21,
          total_pages: 2,
        },
      },
    })
  })

  await page.goto('/families')
  await expect(page.getByText('第一页家庭')).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('这一页没有家长账号')).toBeVisible()
  await expect(page.locator('.pagination')).toBeVisible()
  const pageSizeSelect = page.locator('.pagination select')
  await pageSizeSelect.selectOption('50')
  await expect(pageSizeSelect).toHaveValue('50')
  await expect(page.getByText('第一页家庭')).toBeVisible()
  expect(pageSizeRequestCount).toBe(1)
})

test('keeps release pagination visible when the current page is empty', async ({
  page,
}) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/releases**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    const requestURL = new URL(route.request().url())
    const pageNumber = Number(requestURL.searchParams.get('page') ?? '1')
    await fulfillJSON(route, 200, {
      data: {
        releases:
          pageNumber === 1 ? [releasePayload('1.0.0')] : [],
        pagination: {
          page: pageNumber,
          page_size: 20,
          total: 21,
          total_pages: 2,
        },
      },
    })
  })

  await page.goto('/releases')
  await expect(page.getByText('1.0.0', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('这一页没有更新记录')).toBeVisible()
  await expect(page.locator('.pagination')).toBeVisible()
  await expect(page.getByRole('button', { name: '返回上一页' })).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '上一页', exact: true }),
  ).toBeEnabled()
})

test('keeps the current release page size when a page-size request fails', async ({
  page,
}) => {
  await useAdminSession(page)
  let pageSizeRequestCount = 0
  await page.route('**/api/v1/admin/releases**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    const requestURL = new URL(route.request().url())
    const pageSize = Number(requestURL.searchParams.get('page_size') ?? '20')
    if (pageSize === 50) {
      pageSizeRequestCount += 1
      await fulfillJSON(route, 503, {
        error: { code: 'service_unavailable', message: '暂时无法读取' },
      })
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        releases: [releasePayload('2.0.0')],
        pagination: {
          page: 1,
          page_size: pageSize,
          total: 1,
          total_pages: 1,
        },
      },
    })
  })

  await page.goto('/releases')
  await expect(page.getByText('2.0.0', { exact: true })).toBeVisible()
  const pageSizeSelect = page.locator('.pagination select')
  await pageSizeSelect.selectOption('50')
  await expect(pageSizeSelect).toHaveValue('20')
  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  expect(pageSizeRequestCount).toBe(1)
})

test('clamps to the previous release page after deleting the last item', async ({
  page,
}) => {
  await useAdminSession(page)
  let deleted = false
  await page.route('**/api/v1/admin/releases**', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    const requestURL = new URL(route.request().url())
    const pageNumber = Number(requestURL.searchParams.get('page') ?? '1')
    const total = deleted ? 1 : 21
    const totalPages = deleted ? 1 : 2
    let releases: Record<string, unknown>[] = []
    if (pageNumber === 1) {
      releases = [releasePayload('1.0.0')]
    } else if (!deleted) {
      releases = [releasePayload('1.0.1')]
    }
    await fulfillJSON(route, 200, {
      data: {
        releases,
        pagination: {
          page: pageNumber,
          page_size: 20,
          total,
          total_pages: totalPages,
        },
      },
    })
  })
  await page.route('**/api/v1/admin/releases/release-1.0.1', async (route) => {
    if (route.request().method() !== 'DELETE') {
      await fulfillJSON(route, 405, {
        error: { code: 'method_not_allowed', message: '' },
      })
      return
    }
    deleted = true
    await fulfillJSON(route, 200, { data: {} })
  })

  await page.goto('/releases')
  await expect(page.getByText('1.0.0', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '下一页' }).click()
  await expect(page.getByText('1.0.1', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '删除' }).click()
  await page.getByRole('button', { name: '删除版本' }).click()
  await expect(page.getByText('1.0.0', { exact: true })).toBeVisible()
  await expect(page.getByText('第 1 / 1 页')).toBeVisible()
})

test('separates a successful upload from a failed release registration', async ({
  page,
}) => {
  await useAdminSession(page)
  let registrationAttempts = 0
  await page.route('**/api/v1/admin/storage/files', async (route) => {
    await fulfillJSON(route, 200, { data: { files: [] } })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    await fulfillJSON(route, 200, {
      data: { file: uploadedFile('app-3.0.0.apk', '3.0.0', 'client', 'android') },
    })
  })
  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'GET') {
      await fulfillJSON(route, 200, {
        data: {
          releases: [],
          pagination: { page: 1, page_size: 100, total: 0, total_pages: 0 },
        },
      })
      return
    }
    registrationAttempts += 1
    if (registrationAttempts === 1) {
      await fulfillJSON(route, 503, {
        error: { code: 'service_unavailable', message: '登记服务暂时不可用' },
      })
      return
    }
    await fulfillJSON(route, 200, { data: {} })
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })
  await page.route('**/api/v1/admin/storage/index/refresh', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })

  await page.goto('/downloads')
  const dialog = await fillUploadForm(page, {
    filename: 'app-3.0.0.apk',
    version: '3.0.0',
    kind: 'client',
    platform: 'android',
    notes: '修复登录并优化启动速度。',
  })
  await dialog.getByRole('button', { name: '上传并登记版本' }).click()
  await expect(
    dialog.getByText('文件已上传，但版本登记失败。文件不会丢失，可以重试登记。'),
  ).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: '重试登记版本' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '取消' })).toBeDisabled()
  await expect(
    dialog.getByText('文件已经上传，请先完成登记、索引同步和列表刷新，再关闭窗口。'),
  ).toBeVisible()
  expect(registrationAttempts).toBe(1)
})

test('keeps a completed release open when index refresh fails and retries only the failed stage', async ({
  page,
}) => {
  await useAdminSession(page)
  let indexRefreshAttempts = 0
  await page.route('**/api/v1/admin/storage/files', async (route) => {
    await fulfillJSON(route, 200, { data: { files: [] } })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    await fulfillJSON(route, 200, {
      data: { file: uploadedFile('app-4.0.0.apk', '4.0.0', 'client', 'android') },
    })
  })
  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'GET') {
      await fulfillJSON(route, 200, {
        data: {
          releases: [],
          pagination: { page: 1, page_size: 100, total: 0, total_pages: 0 },
        },
      })
      return
    }
    await fulfillJSON(route, 200, { data: {} })
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })
  await page.route('**/api/v1/admin/storage/index/refresh', async (route) => {
    indexRefreshAttempts += 1
    if (indexRefreshAttempts === 1) {
      await fulfillJSON(route, 503, {
        error: {
          code: 'path_unavailable',
          message: '/srv/releases/.index/publish-index.json is unavailable',
        },
      })
      return
    }
    await fulfillJSON(route, 200, indexStatusPayload())
  })

  await page.goto('/downloads')
  const dialog = await fillUploadForm(page, {
    filename: 'app-4.0.0.apk',
    version: '4.0.0',
    kind: 'client',
    platform: 'android',
    notes: '新增设备配对与语音播报。',
  })
  await dialog.getByRole('button', { name: '上传并登记版本' }).click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('版本已经登记，但发布索引同步失败。'))
    .toBeVisible()
  await expect(dialog.getByText('/srv/releases/.index/publish-index.json'))
    .toHaveCount(0)
  await dialog.getByRole('button', { name: '重试刷新索引' }).click()
  await expect(dialog).toHaveCount(0)
  expect(indexRefreshAttempts).toBe(2)
})

test('reloads the file list after a completed release when only the list refresh failed', async ({
  page,
}) => {
  await useAdminSession(page)
  let fileListRequests = 0
  let reloadedAfterUpload = false
  const storedFile = uploadedFile(
    'app-5.0.0.apk',
    '5.0.0',
    'client',
    'android',
  )
  await page.route('**/api/v1/admin/storage/files', async (route) => {
    fileListRequests += 1
    if (fileListRequests === 2) {
      reloadedAfterUpload = true
      await fulfillJSON(route, 503, {
        error: { code: 'service_unavailable', message: '暂时无法读取文件列表' },
      })
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        files: fileListRequests < 3 ? [] : [storedFile],
      },
    })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    await fulfillJSON(route, 200, { data: { file: storedFile } })
  })
  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'GET') {
      await fulfillJSON(route, 200, {
        data: {
          releases: [],
          pagination: { page: 1, page_size: 100, total: 0, total_pages: 0 },
        },
      })
      return
    }
    await fulfillJSON(route, 200, { data: {} })
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })
  await page.route('**/api/v1/admin/storage/index/refresh', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })

  await page.goto('/downloads')
  const dialog = await fillUploadForm(page, {
    filename: 'app-5.0.0.apk',
    version: '5.0.0',
    kind: 'client',
    platform: 'android',
    notes: '优化下载更新体验。',
  })
  await dialog.getByRole('button', { name: '上传并登记版本' }).click()
  await expect(dialog).toBeVisible()
  await expect(
    dialog.getByText(/版本已经完成，但文件列表刷新失败/),
  ).toBeVisible()
  expect(reloadedAfterUpload).toBe(true)
  await dialog.getByRole('button', { name: '重新读取文件' }).click()
  await expect(dialog).toHaveCount(0)
  expect(fileListRequests).toBeGreaterThanOrEqual(3)
})

test('registers an uploaded file under its explicit release channel', async ({ page }) => {
  await useAdminSession(page)
  const registered: Record<string, unknown>[] = []
  await page.route('**/api/v1/admin/storage/files', async (route) => {
    await fulfillJSON(route, 200, { data: { files: [] } })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    const file = {
      ...uploadedFile('app-beta.apk', '7.0.0', 'client', 'android'),
      relative_path: '7.0.0/beta/android/apk/app-beta.apk',
      directory: '7.0.0/beta/android/apk',
      channel: 'beta',
      release: {
        version: '7.0.0',
        channel: 'beta',
        kind: 'client',
        platform: 'android',
      },
    }
    await fulfillJSON(route, 200, { data: { file } })
  })
  await page.route('**/api/v1/admin/releases**', async (route) => {
    if (route.request().method() === 'GET') {
      const requestURL = new URL(route.request().url())
      if (requestURL.pathname !== '/api/v1/admin/releases') {
        await route.fallback()
        return
      }
      await fulfillJSON(route, 200, {
        data: {
          releases: [],
          pagination: { page: 1, page_size: 100, total: 0, total_pages: 0 },
        },
      })
      return
    }
    registered.push(JSON.parse(route.request().postData() ?? '{}'))
    await fulfillJSON(route, 201, { data: {} })
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })
  await page.route('**/api/v1/admin/storage/index/refresh', async (route) => {
    await fulfillJSON(route, 200, indexStatusPayload())
  })

  await page.goto('/downloads')
  const dialog = await fillUploadForm(page, {
    filename: 'app-beta.apk',
    version: '7.0.0',
    kind: 'client',
    platform: 'android',
    notes: '测试渠道验证。',
  })
  await dialog.getByLabel('更新渠道').selectOption('beta')
  await dialog.getByRole('button', { name: '上传并登记版本' }).click()

  await expect.poll(() => registered.length).toBe(1)
  expect(registered[0]?.channel).toBe('beta')
})

test('keeps release pagination available on a genuinely empty release catalog', async ({
  page,
}) => {
  await useAdminSession(page)
  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    await fulfillJSON(route, 200, {
      data: {
        releases: [],
        pagination: {
          page: 1,
          page_size: 20,
          total: 0,
          total_pages: 0,
        },
      },
    })
  })

  await page.goto('/releases')
  await expect(page.getByText('还没有更新记录')).toBeVisible()
  await expect(page.locator('.pagination')).toBeVisible()
  await expect(page.locator('.pagination select')).toHaveValue('20')
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByRole('button', { name: '登记新版本' }).last()).toBeVisible()
})

test('offers a concrete recovery path for a draft without release notes', async ({
  page,
}) => {
  await useAdminSession(page)
  const draft = {
    ...releasePayload('6.0.0'),
    status: 'draft',
    release_notes: '',
    published_at: '',
  }
  let deleted = false
  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    if (route.request().method() === 'GET') {
      await fulfillJSON(route, 200, {
        data: {
          releases: [draft],
          pagination: {
            page: 1,
            page_size: 20,
            total: 1,
            total_pages: 1,
          },
        },
      })
      return
    }
    await fulfillJSON(route, 200, { data: {} })
  })
  await page.route('**/api/v1/admin/releases/release-6.0.0', async (route) => {
    if (route.request().method() !== 'DELETE') {
      await fulfillJSON(route, 405, {
        error: { code: 'method_not_allowed', message: '' },
      })
      return
    }
    deleted = true
    await fulfillJSON(route, 200, { data: {} })
  })

  await page.goto('/releases')
  await page.getByRole('button', { name: '补充更新说明' }).click()
  await expect(page.getByRole('heading', { name: '补充版本 6.0.0 的更新说明' }))
    .toBeVisible()
  await expect(page.getByText('删除并重新登记')).toBeVisible()
  await page.getByRole('button', { name: '删除并重新登记' }).click()
  const createDialog = page.getByRole('dialog', { name: '登记新版本' })
  await expect(createDialog).toBeVisible()
  await expect(createDialog.locator('input[type="text"]').first()).toHaveValue(
    '6.0.0',
  )
  await expect(createDialog.getByLabel('下载地址')).toHaveValue(
    'https://download.example.test/6.0.0.apk',
  )
  expect(deleted).toBe(true)
})

test('renders and operates same-version artifacts by record identity', async ({
  page,
}) => {
  await useAdminSession(page)
  const apk = releaseArtifactPayload('0.29.0', 'client', 'android', 'stable')
  const adminWeb = releaseArtifactPayload('0.29.0', 'client', 'all', 'stable')
  const published: string[] = []
  const deleted: string[] = []
  let currentReleases = [apk, adminWeb]

  await page.route(/\/api\/v1\/admin\/releases(?:\?.*)?$/, async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback()
      return
    }
    await fulfillJSON(route, 200, {
      data: {
        releases: currentReleases,
        pagination: {
          page: 1,
          page_size: 20,
          total: currentReleases.length,
          total_pages: 1,
        },
      },
    })
  })
  await page.route('**/api/v1/admin/releases/*/publish', async (route) => {
    const releaseID = new URL(route.request().url()).pathname.split('/')[5]
    const publishID = releaseID ?? ''
    published.push(publishID)
    currentReleases = currentReleases.map((release) =>
      release.id === publishID ? { ...release, status: 'published' } : release,
    )
    await fulfillJSON(route, 200, { data: { published: true } })
  })
  await page.route('**/api/v1/admin/releases/*', async (route) => {
    const requestURL = new URL(route.request().url())
    if (
      route.request().method() !== 'DELETE' ||
      !requestURL.pathname.startsWith('/api/v1/admin/releases/')
    ) {
      await route.fallback()
      return
    }
    const releaseID = new URL(route.request().url()).pathname.split('/').at(-1) ?? ''
    deleted.push(releaseID)
    currentReleases = currentReleases.filter((release) => release.id !== releaseID)
    await fulfillJSON(route, 200, { data: { deleted: true } })
  })

  await page.goto('/releases')
  await expect(page.locator('table tbody tr')).toHaveCount(2)
  await expect(page.getByText('Android', { exact: true })).toBeVisible()
  await expect(page.getByText('全部平台', { exact: true })).toBeVisible()

  const firstRow = page
    .locator('table tbody tr')
    .filter({ hasText: 'Android' })
    .first()
  await firstRow.getByRole('button', { name: '发布' }).click()
  await page.getByRole('button', { name: '确认发布' }).click()
  await expect.poll(() => published.length).toBe(1)
  expect(published[0]).toBe('release-0.29.0-stable-client-android')

  const secondRow = page
    .locator('table tbody tr')
    .filter({ hasText: '全部平台' })
    .first()
  await secondRow.getByRole('button', { name: '删除' }).click()
  await page.getByRole('button', { name: '删除版本' }).click()
  await expect.poll(() => deleted.length).toBe(1)
  expect(deleted[0]).toBe('release-0.29.0-stable-client-all')
  await expect(page.locator('table tbody tr')).toHaveCount(1)
})

test('treats a failed replacement upload as a conflict without sending a file', async ({
  page,
}) => {
  await useAdminSession(page)
  let uploadRequests = 0
  const indexedFile = {
    id: 'existing-client',
    name: 'sprout-parent-app-0.12.3.apk',
    file_name: 'sprout-parent-app-0.12.3.apk',
    relative_path: '0.12.3/stable/android/apk/sprout-parent-app-0.12.3.apk',
    directory: '0.12.3/stable/android/apk',
    size_bytes: 1024,
    modified_at: '2026-10-18T09:00:00Z',
    sha256: 'a'.repeat(64),
    download_url:
      'https://download.example.test/0.12.3/stable/android/apk/sprout-parent-app-0.12.3.apk',
    version: '0.12.3',
    kind: 'client',
    platform: 'android',
    is_indexed: true,
  }
  await page.route('**/api/v1/admin/storage/files', async (route) => {
    await fulfillJSON(route, 200, { data: { files: [indexedFile] } })
  })
  await page.route('**/api/v1/admin/storage/files/upload', async (route) => {
    uploadRequests += 1
    await fulfillJSON(route, 200, { data: {} })
  })
  await page.route('**/api/v1/admin/storage/index/status', async (route) => {
    await fulfillJSON(route, 200, {
      data: {
        available: true,
        refreshed_at: '2026-10-18T09:00:00Z',
        indexed_file_count: 1,
        pending_file_count: 0,
      },
    })
  })

  await page.goto('/downloads')
  await page.getByRole('button', { name: '上传文件' }).click()
  const dialog = page.getByRole('dialog', { name: '上传到下载服务器' })
  await dialog.getByLabel('本地文件').setInputFiles({
    name: 'sprout-parent-app-0.12.3.apk',
    mimeType: 'application/vnd.android.package-archive',
    buffer: Buffer.from('replacement'),
  })
  await dialog.getByLabel('文件名').fill('sprout-parent-app-0.12.3.apk')
  await dialog.getByLabel('版本号').fill('0.12.3')
  await dialog.getByLabel('更新渠道').selectOption('stable')
  await dialog.getByLabel('文件类型').selectOption('client')
  await dialog.getByLabel('适用平台').selectOption('android')
  await dialog.getByLabel('更新说明').fill('尝试覆盖。')

  await expect(
    dialog.getByText(
      '这个版本目录下已经有同名文件。为避免已校验文件被替换，请调整渠道、平台或文件名。',
    ),
  ).toBeVisible()
  await expect(dialog.getByRole('button', { name: '上传并登记版本' })).toBeDisabled()
  expect(uploadRequests).toBe(0)
})
