import { expect, test, type Page } from '@playwright/test'

async function seedSession(page: Page): Promise<void> {
  await page.addInitScript(() => {
    globalThis.sessionStorage.setItem('sprout.admin.accessToken', 'stale-access-token')
    globalThis.sessionStorage.setItem('sprout.admin.refreshToken', 'refresh-token')
  })
}

test('keeps credentials and offers retry when session restoration is unavailable', async ({
  page,
}) => {
  await seedSession(page)
  await page.route('**/api/v1/auth/refresh', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'service_unavailable', message: 'service unavailable' },
      }),
    })
  })
  await page.route('**/api/v1/auth/me', async (route) => {
    await route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'service_unavailable', message: 'service unavailable' },
      }),
    })
  })

  page.on('response', (response) => {
    if (response.url().includes('/api/v1/auth/')) {
      console.log('auth response', response.url(), response.status())
    }
  })
  await page.goto('/')

  await expect(page.getByRole('heading', { name: '暂时无法连接管理服务' })).toBeVisible()
  await expect(page.getByRole('button', { name: '重新连接' })).toBeVisible()
  await expect
    .poll(() => page.evaluate(() => sessionStorage.getItem('sprout.admin.refreshToken')))
    .toBe('refresh-token')
})

test('clears credentials when the session is explicitly rejected', async ({ page }) => {
  await seedSession(page)
  await page.route('**/api/v1/auth/refresh', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'unauthenticated', message: 'refresh token expired' },
      }),
    })
  })
  await page.route('**/api/v1/auth/me', async (route) => {
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'unauthenticated', message: 'session expired' },
      }),
    })
  })

  await page.goto('/')

  await expect(page).toHaveURL(/\/login$/)
  await expect
    .poll(() => page.evaluate(() => sessionStorage.getItem('sprout.admin.refreshToken')))
    .toBeNull()
})
