import { expect, test, type Page } from '@playwright/test'

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
            admin_permissions: ['settings.read', 'settings.manage'],
          },
        },
      }),
    })
  })
}

test('keeps settings read-only when the initial load fails', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/settings', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: {
          code: 'service_unavailable',
          message: 'service unavailable',
        },
      }),
    })
  })

  await page.goto('/settings')

  await expect(page.getByText('服务暂时不可用，请稍后重试')).toBeVisible()
  await expect(page.getByRole('button', { name: '重新加载' })).toBeVisible()
  await expect(page.getByRole('button', { name: '保存系统设置' })).toBeDisabled()
  await expect(page.getByRole('heading', { name: '系统设置' })).toBeVisible()
  await expect(page.getByLabel('默认同时对话数量')).toHaveCount(0)
})

test('allows editing after the settings load succeeds', async ({ page }) => {
  await useAdminSession(page)
  await page.route('**/api/v1/admin/settings', async (route) => {
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
            account: {
              registration_enabled: true,
              phone_verification_required: false,
              email_login_enabled: true,
            },
            safety: {
              minor_mode_default: true,
              output_moderation_enabled: true,
              crisis_intervention_enabled: true,
            },
            voice: {
              continuous_conversation_enabled: true,
              idle_window_seconds: 8,
              barge_in_enabled: true,
              far_field_enabled: true,
            },
            update: {
              channel: 'stable',
              min_client_version: '0.22.0',
              force_upgrade_below: '0.20.0',
            },
            retention: {
              audio_days: 0,
              image_days: 0,
              conversation_days: 30,
              usage_report_days: 365,
            },
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

  await expect(page.getByLabel('默认同时对话数量')).toBeVisible()
  await expect(
    page.locator('header').getByRole('button', { name: '保存系统设置' }),
  ).toBeEnabled()
})
