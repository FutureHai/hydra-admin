import { expect, test } from '@playwright/test'

test('production CSP, login, Client CRUD, one-time Secret and audit', async ({ page }) => {
  await page.addInitScript(() => {
    ;(window as typeof window & { __cspViolations?: string[] }).__cspViolations = []
    window.addEventListener('securitypolicyviolation', event => {
      ;(window as typeof window & { __cspViolations?: string[] }).__cspViolations?.push(`${event.violatedDirective}: ${event.blockedURI} ${event.sourceFile}:${event.lineNumber} ${event.sample}`)
    })
  })

  await page.goto('/clients')
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: '管理员登录' })).toBeVisible()
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('test-only-password')
  await page.getByRole('button', { name: /登\s*录/ }).click()
  await expect(page).toHaveURL(/127\.0\.0\.1:18085\/$/)

  await page.goto('/clients')
  await expect(page.getByRole('heading', { name: 'Client', exact: true })).toBeVisible()
  await page.getByRole('link', { name: '创建 Client' }).click()
  await page.locator('.ant-form-item').filter({ hasText: 'client id' }).locator('input').fill('browser-fixture-client')
  await page.locator('.ant-form-item').filter({ hasText: 'grant types' }).getByRole('combobox').click()
  await page.getByText('client_credentials', { exact: true }).last().click()
  await page.getByRole('button', { name: '检查创建摘要' }).click()
  await expect(page.getByText('确认创建 Client')).toBeVisible()
  await page.getByRole('button', { name: '确认创建', exact: true }).click()
  await expect(page.getByText('Client 已创建')).toBeVisible()
  const secret = await page.getByLabel('一次性 Client Secret').inputValue()
  expect(secret.length).toBeGreaterThan(20)
  await page.getByRole('button', { name: '已保存，关闭' }).click()
  await expect(page).toHaveURL(/\/clients\/browser-fixture-client$/)
  await expect(page.getByText('Secret 轮换不可用')).toBeVisible()
  await expect(page.getByLabel('一次性 Client Secret')).toHaveCount(0)
  await page.reload()
  await expect(page.getByText('Secret 轮换不可用')).toBeVisible()
  expect(await page.locator('body').textContent()).not.toContain(secret)
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])

  await page.getByRole('button', { name: /编\s*辑/ }).click()
  await page.getByRole('dialog', { name: '编辑 Client' }).getByRole('textbox', { name: 'Client 名称' }).fill('Browser test client')
  await page.getByRole('button', { name: '检查修改摘要' }).click()
  await page.getByRole('button', { name: '提交 JSON Patch' }).click()
  await expect(page.getByRole('heading', { name: 'Browser test client' })).toBeVisible()
  await page.getByRole('tab', { name: '只读 JSON' }).click()
  await expect(page.getByText('preserve-me')).toBeVisible()

  await page.getByRole('button', { name: /删\s*除/ }).click()
  await expect(page.getByRole('button', { name: '二次确认删除' })).toBeDisabled()
  await page.getByLabel('键入 Client ID 确认删除').fill('browser-fixture-client')
  await page.getByRole('button', { name: '二次确认删除' }).click()
  await expect(page).toHaveURL(/\/clients$/)
  await page.goto('/audit')
  await expect(page.getByRole('heading', { name: '审计记录' })).toBeVisible()
  await expect(page.getByText('client.create')).toBeVisible()
  await expect(page.getByText('client.update')).toBeVisible()
  await expect(page.getByText('client.delete')).toBeVisible()

  for (const port of [18086, 18087]) {
    const rejected = await page.request.post(`http://127.0.0.1:${port}/api/v1/auth/login`, {
      headers: { Origin: 'http://127.0.0.1:18085', 'X-Forwarded-For': '203.0.113.250' },
      data: { username: 'invalid-test-user', password: 'invalid-test-password' },
    })
    expect(rejected.status()).toBe(403)
    const audit = await page.request.get('/api/v1/audit?action=login&limit=10')
    expect(audit.ok()).toBe(true)
    const events = (await audit.json()) as { items: { outcome: string; source_ip: string }[] }
    expect(events.items[0]).toMatchObject({ outcome: 'failure', source_ip: '127.0.0.1' })
  }

  const violations = await page.evaluate(() => (window as typeof window & { __cspViolations?: string[] }).__cspViolations ?? [])
  const styleState = await page.evaluate(() => ({
    metaNonce: Boolean(document.querySelector('meta[name="csp-nonce"]')?.getAttribute('content')),
    styleCount: document.querySelectorAll('style').length,
    withNonce: [...document.querySelectorAll('style')].filter(style => Boolean(style.nonce)).length,
    withoutNonce: [...document.querySelectorAll('style')].filter(style => !style.nonce).slice(0, 4).map(style => style.outerHTML.slice(0, 160)),
  }))
  expect(styleState.withoutNonce, JSON.stringify(styleState)).toEqual([])
  expect(violations).toEqual([])
  expect(styleState.withNonce).toBeGreaterThan(0)
})
