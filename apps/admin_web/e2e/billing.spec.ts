import { expect, test, type Page, type Route } from '@playwright/test'

// 计费管理页的浏览器级验收：金额一律是人民币，只读管理员看不到处理按钮，
// 有管理权限的管理员批准退款时发出的内容正确，空状态与加载失败都能给出
// 可执行的下一步。
//
// 页面已登记到管理端路由，用例按管理员真实路径导航进入，不再自建挂载入口。

interface MountedBillingPage {
  path: string
  heading: string
}

const ORDERS_PAGE: MountedBillingPage = {
  path: '/billing/orders',
  heading: '订单',
}

const REFUNDS_PAGE: MountedBillingPage = {
  path: '/billing/refunds',
  heading: '退款审批',
}

const PLANS_PAGE: MountedBillingPage = {
  path: '/billing/plans',
  heading: '套餐',
}

const RECONCILIATION_PAGE: MountedBillingPage = {
  path: '/billing/reconciliation',
  heading: '对账',
}

async function useAdminSession(page: Page, permissions: string[]): Promise<void> {
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

function jsonResponse(
  data: unknown,
  status = 200,
): { status: number; contentType: string; body: string } {
  return {
    status,
    contentType: 'application/json',
    body: JSON.stringify({ schema_version: '1.0.0', data, error: null }),
  }
}

function serviceUnavailable(): { status: number; contentType: string; body: string } {
  return {
    status: 503,
    contentType: 'application/json',
    body: JSON.stringify({
      error: { code: 'service_unavailable', message: 'service unavailable' },
    }),
  }
}

function orderRecord(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'order-1',
    order_no: 'SP20261020A1B2C3',
    parent_account_id: 'parent-1',
    plan_code: 'starter_monthly',
    plan_display_name: '初芽体验包',
    billing_period: 'monthly',
    period_months: 1,
    unit_price_cny_cents: 12000,
    discount_cny_cents: 2000,
    payable_cny_cents: 10000,
    paid_cny_cents: 10000,
    refunded_cny_cents: 0,
    status: 'paid',
    expires_at: '2026-10-20T12:30:00Z',
    paid_at: '2026-10-20T10:05:00Z',
    created_at: '2026-10-20T10:00:00Z',
    ...overrides,
  }
}

function refundRecord(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'refund-1',
    refund_no: 'RF20261020K9M8N7',
    order_id: 'order-1',
    order_no: 'SP20261020A1B2C3',
    amount_cny_cents: 9900,
    reason: '家里暂时不需要这个套餐了',
    status: 'requested',
    requested_at: '2026-10-20T11:00:00Z',
    ...overrides,
  }
}

function planRecord(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'plan-1',
    code: 'starter_monthly',
    display_name: '初芽体验包',
    description: '每月发放一次陪伴时长',
    billing_period: 'monthly',
    period_months: 1,
    price_cny_cents: 990,
    original_price_cny_cents: 1990,
    status: 'active',
    sort_order: 1,
    version: 3,
    published_at: '2026-10-01T00:00:00Z',
    ...overrides,
  }
}

function reconciliationRun(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: 'run-1',
    channel: 'wechat',
    business_date: '2026-10-19',
    status: 'completed',
    channel_total_cny_cents: 25800,
    platform_total_cny_cents: 25900,
    difference_cny_cents: -100,
    matched_count: 12,
    missing_count: 1,
    extra_count: 0,
    started_at: '2026-10-20T01:00:00Z',
    finished_at: '2026-10-20T01:02:00Z',
    ...overrides,
  }
}

interface BillingRouteOptions {
  orders?: Array<Record<string, unknown>>
  ordersUnavailable?: boolean
  refunds?: Array<Record<string, unknown>>
  refundsUnavailable?: boolean
  plans?: Array<Record<string, unknown>>
  planUpdateConflict?: boolean
  runs?: Array<Record<string, unknown>>
}

interface RecordedRequest {
  method: string
  path: string
  body: unknown
}

async function setupBillingRoutes(
  page: Page,
  options: BillingRouteOptions = {},
): Promise<{ requests: RecordedRequest[] }> {
  const requests: RecordedRequest[] = []
  const orders = options.orders ?? [orderRecord()]

  await page.route('**/api/v1/admin/billing/**', async (route: Route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() !== 'GET') {
      const rawBody = request.postData()
      requests.push({
        method: request.method(),
        path,
        body: rawBody === null || rawBody === '' ? {} : safeJsonParse(rawBody),
      })
    }

    if (path === '/api/v1/admin/billing/orders') {
      if (options.ordersUnavailable === true) {
        await route.fulfill(serviceUnavailable())
        return
      }
      await route.fulfill(
        jsonResponse({
          orders,
          pagination: {
            page: 1,
            page_size: 20,
            total: orders.length,
            total_pages: orders.length === 0 ? 0 : 1,
          },
        }),
      )
      return
    }

    if (path.startsWith('/api/v1/admin/billing/orders/')) {
      const orderNo = decodeURIComponent(path.split('/').pop() ?? '')
      const order = orders.find((item) => item.order_no === orderNo) ?? orderRecord()
      await route.fulfill(
        jsonResponse({
          order,
          payments: [
            {
              id: 'payment-1',
              channel: 'wechat',
              channel_trade_no: '4200002506202610201234',
              amount_cny_cents: 10000,
              status: 'succeeded',
              created_at: '2026-10-20T10:01:00Z',
            },
          ],
          refunds: [],
        }),
      )
      return
    }

    if (path === '/api/v1/admin/billing/refunds') {
      if (options.refundsUnavailable === true) {
        await route.fulfill(serviceUnavailable())
        return
      }
      await route.fulfill(jsonResponse({ refunds: options.refunds ?? [refundRecord()] }))
      return
    }

    if (path.endsWith('/approve') || path.endsWith('/reject')) {
      const decidedStatus = path.endsWith('/approve') ? 'approved' : 'rejected'
      await route.fulfill(jsonResponse({ refund: refundRecord({ status: decidedStatus }) }))
      return
    }

    if (path === '/api/v1/admin/billing/plans') {
      await route.fulfill(jsonResponse({ plans: options.plans ?? [planRecord()] }))
      return
    }

    if (path.startsWith('/api/v1/admin/billing/plans/') && request.method() === 'PUT') {
      if (options.planUpdateConflict === true) {
        await route.fulfill({
          status: 409,
          contentType: 'application/json',
          body: JSON.stringify({
            error: { code: 'plan_version_conflict', message: 'version conflict' },
          }),
        })
        return
      }
      await route.fulfill(jsonResponse({ plan: planRecord({ version: 4 }) }))
      return
    }

    if (path === '/api/v1/admin/billing/reconciliation' && request.method() === 'GET') {
      await route.fulfill(
        jsonResponse({ runs: options.runs ?? [reconciliationRun()] }),
      )
      return
    }

    if (path === '/api/v1/admin/billing/reconciliation' && request.method() === 'POST') {
      await route.fulfill(
        jsonResponse({
          run: reconciliationRun({ id: 'run-new', status: 'running', finished_at: '' }),
        }),
      )
      return
    }

    await route.fulfill(jsonResponse({}))
  })

  return { requests }
}

function safeJsonParse(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}

/// 用真实路由打开一个计费页面，返回 true 表示页面渲染出来了。
///
/// 这些页面已经登记到管理端路由，所以用例走导航而不是自己挂载组件，
/// 这样测的就是管理员真正会看到的路径。
async function openBillingPage(
  page: Page,
  mount: MountedBillingPage,
  permissions: string[],
): Promise<true | string> {
  await useAdminSession(page, permissions)
  await page.route('**/api/v1/admin/overview', async (route) => {
    await route.fulfill(jsonResponse({ overview: {} }))
  })
  await page.goto(mount.path)
  await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()
  const heading = page.getByRole('heading', { level: 1 }).first()
  const visible = await heading.isVisible().catch(() => false)
  if (!visible) {
    return '页面没有渲染出标题'
  }
  return true
}

test.describe('计费管理页', () => {
  test('订单列表只显示人民币，并可展开详情', async ({ page }) => {
    await setupBillingRoutes(page)
    expect(await openBillingPage(page, ORDERS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByRole('heading', { name: '订单', level: 1 })).toBeVisible()
    await expect(page.getByText('SP20261020A1B2C3')).toBeVisible()
    await expect(page.getByRole('cell', { name: '已支付' })).toBeVisible()
    // 10000 分显示为 ¥100.00，绝不出现分值或美元符号。
    await expect(page.getByRole('cell', { name: '¥100.00' }).first()).toBeVisible()
    await expect(page.locator('body')).not.toContainText('$')

    await page.getByRole('button', { name: '查看订单 SP20261020A1B2C3' }).click()
    const drawer = page.getByRole('dialog', { name: '订单 SP20261020A1B2C3' })
    await expect(drawer).toBeVisible()
    await expect(drawer.getByText('¥120.00')).toBeVisible()
    await expect(drawer.getByText('¥20.00')).toBeVisible()
    await expect(drawer.getByText('微信支付 · ¥100.00')).toBeVisible()
    await expect(
      drawer.getByText('这笔订单没有退款记录。家长提交退款申请后，会在这里显示处理进度。'),
    ).toBeVisible()
  })

  test('只有查看权限的管理员看不到退款审批按钮', async ({ page }) => {
    const { requests } = await setupBillingRoutes(page)
    expect(await openBillingPage(page, REFUNDS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByRole('heading', { name: '退款审批', level: 1 })).toBeVisible()
    await expect(page.getByText('RF20261020K9M8N7')).toBeVisible()
    await expect(page.getByText('¥99.00')).toBeVisible()
    await expect(page.getByRole('button', { name: '批准退款' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '拒绝退款' })).toHaveCount(0)
    await expect(page.getByText('你当前只有查看权限', { exact: false })).toBeVisible()
    expect(requests).toHaveLength(0)
  })

  test('只有查看权限的管理员看不到保存套餐按钮', async ({ page }) => {
    const { requests } = await setupBillingRoutes(page)
    expect(await openBillingPage(page, PLANS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByRole('heading', { name: '套餐', level: 1 })).toBeVisible()
    await expect(page.getByText('初芽体验包')).toBeVisible()
    await expect(page.getByText('¥9.90')).toBeVisible()
    await expect(page.getByRole('button', { name: '新建套餐' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '修改套餐 初芽体验包' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '保存套餐' })).toHaveCount(0)
    expect(requests).toHaveLength(0)
  })

  test('有管理权限的管理员批准退款，内容只带退款单号', async ({ page }) => {
    const { requests } = await setupBillingRoutes(page)
    expect(
      await openBillingPage(page, REFUNDS_PAGE, ['billing.read', 'billing.manage']),
    ).toBe(true)

    await page.getByRole('button', { name: '批准退款 RF20261020K9M8N7' }).click()
    const dialog = page.getByRole('dialog', { name: '批准退款申请' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toContainText('¥99.00')
    await dialog.getByRole('button', { name: '批准退款' }).click()

    await expect(page.getByText('退款申请已批准。')).toBeVisible()
    await expect(page.getByText('RF20261020K9M8N7')).toHaveCount(0)

    const approvals = requests.filter((request) => request.path.endsWith('/approve'))
    expect(approvals).toHaveLength(1)
    expect(approvals[0]).toMatchObject({
      method: 'POST',
      path: '/api/v1/admin/billing/refunds/RF20261020K9M8N7/approve',
    })
    // 审批只按退款单号流转，不接受客户端传来的金额，避免被篡改。
    expect(approvals[0]?.body).toEqual({})
  })

  test('套餐被其他管理员改过时要求重新加载', async ({ page }) => {
    const { requests } = await setupBillingRoutes(page, { planUpdateConflict: true })
    expect(
      await openBillingPage(page, PLANS_PAGE, ['billing.read', 'billing.manage']),
    ).toBe(true)

    await page.getByRole('button', { name: '修改套餐 初芽体验包' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    // 售价按元输入，提交时必须换算成整数分。
    await dialog.getByLabel('售价（元）').fill('12.34')
    await dialog.getByRole('button', { name: '保存套餐' }).click()

    await expect(page.getByText('该套餐已被其他管理员修改，请重新加载。')).toBeVisible()
    await expect(page.getByRole('button', { name: '重新加载套餐' }).first()).toBeVisible()

    const updates = requests.filter((request) => request.method === 'PUT')
    expect(updates).toHaveLength(1)
    expect(updates[0]?.body).toMatchObject({
      price_cny_cents: 1234,
      version: 3,
    })
    // 失败不能被说成保存成功。
    await expect(page.getByText('套餐已保存。')).toHaveCount(0)
  })

  test('对账页把差异显示为带符号的人民币金额', async ({ page }) => {
    await setupBillingRoutes(page)
    expect(await openBillingPage(page, RECONCILIATION_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByRole('heading', { name: '对账', level: 1 })).toBeVisible()
    // 表格里同时有平台入账、渠道账单与带符号的差异，直接用单元格定位。
    await expect(page.getByRole('row', { name: /2026 年 10 月 19 日/ })).toBeVisible()
    await expect(page.getByRole('cell', { name: '-¥1.00' }).first()).toBeVisible()
    await expect(page.getByRole('cell', { name: '¥258.00' })).toBeVisible()
    await expect(page.getByRole('cell', { name: '¥259.00' })).toBeVisible()
    await expect(page.getByRole('button', { name: '开始对账' })).toHaveCount(0)
  })

  test('没有订单时说明原因和下一步', async ({ page }) => {
    await setupBillingRoutes(page, { orders: [] })
    expect(await openBillingPage(page, ORDERS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByText('还没有订单')).toBeVisible()
    await expect(page.getByText('家长下单后会显示在这里。', { exact: false })).toBeVisible()
    await expect(page.getByRole('button', { name: '重新加载订单' })).toBeVisible()
    await expect(page.getByRole('button', { name: '清除筛选' })).toBeVisible()
    await expect(page.locator('table')).toHaveCount(0)
  })

  test('订单加载失败时给出可重试的中文提示', async ({ page }) => {
    await setupBillingRoutes(page, { ordersUnavailable: true })
    expect(await openBillingPage(page, ORDERS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByText('订单暂时没有加载出来')).toBeVisible()
    await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
    await expect(page.getByRole('button', { name: '重新加载订单' })).toBeVisible()
    // 失败状态不能冒充空状态，也不展示原始错误。
    await expect(page.getByText('还没有订单')).toHaveCount(0)
    await expect(page.locator('body')).not.toContainText('service unavailable')
  })

  test('退款列表加载失败时给出可重试的中文提示', async ({ page }) => {
    await setupBillingRoutes(page, { refundsUnavailable: true })
    expect(await openBillingPage(page, REFUNDS_PAGE, ['billing.read'])).toBe(true)

    await expect(page.getByText('退款申请暂时没有加载出来')).toBeVisible()
    await expect(page.getByRole('button', { name: '重新加载退款申请' })).toBeVisible()
    await expect(page.getByText('没有待审批的退款申请')).toHaveCount(0)
  })

  test('没有待审批退款时说明原因', async ({ page }) => {
    await setupBillingRoutes(page, { refunds: [] })
    expect(
      await openBillingPage(page, REFUNDS_PAGE, ['billing.read', 'billing.manage']),
    ).toBe(true)

    await expect(page.getByText('没有待审批的退款申请')).toBeVisible()
    await expect(
      page.getByText('家长在应用里提交退款申请后，会出现在这里等待处理。'),
    ).toBeVisible()
  })
})
