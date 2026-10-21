import { test as base, expect, type Page } from '@playwright/test'

// A local Docker browser can be used on hosts without Chromium's system libraries.
const test = base.extend({
  browser: [async ({ playwright }, use) => {
    const browser = process.env.PLAYWRIGHT_CDP_URL
      ? await playwright.chromium.connectOverCDP(process.env.PLAYWRIGHT_CDP_URL)
      : await playwright.chromium.launch()
    await use(browser)
    await browser.close()
  }, { scope: 'worker' }],
})

async function mockOffice(page: Page, { legacy = false, delayed = false } = {}) {
  const crashes: string[] = []
  page.on('pageerror', error => crashes.push(error.message))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    let body: unknown = {}
    if (url.pathname.endsWith('/auth/status')) body = { authenticated: true, user: { id: 1, name: 'Test', role: 'admin' } }
    else if (url.pathname.endsWith('/sources')) body = [{ id: 2, kind: 'documents', name: 'Pocket Documents' }]
    else if (url.pathname.endsWith('/formats')) body = []
    else if (url.pathname.endsWith('/settings')) body = { title: 'Pocket Office', me: {} }
    else if (url.pathname.endsWith('/open')) {
      const { path, engine } = route.request().postDataJSON()
      const ext = path.split('.').pop()
      body = {
        ...(legacy ? {} : { engine: engine || 'onlyoffice', engines: ['onlyoffice', 'collabora'], collabora_opens: true }),
        api_js: '/test-docs-api.js', config: {}, key: 'test-key', mode: 'view',
        kind: ext === 'pdf' ? 'pdf' : 'word', ext, exports: ['pdf', 'docx'],
        file: { name: path.split('/').pop(), path },
        collabora: { action_url: '/test-collabora', access_token: 'test', access_token_ttl: '0', origin: 'http://127.0.0.1:18187' },
      }
    } else if (url.pathname.endsWith('/client-error')) crashes.push(route.request().postDataJSON().message)
    else if (url.pathname.endsWith('/ls')) body = { entries: [], tree: true, writable: true }
    await route.fulfill({ json: body })
  })
  await page.route('**/test-collabora', route => route.fulfill({ contentType: 'text/html', body: '<p>Collabora test frame</p>' }))
  await page.route('**/test-docs-api.js', async route => {
    if (delayed) await new Promise(resolve => setTimeout(resolve, 200))
    // Match the SDK's DOM ownership contract: replace the target on creation
    // and replace the iframe with a fresh target on destruction.
    await route.fulfill({ contentType: 'application/javascript', body: `
      window.DocsAPI = { DocEditor: function(id, config) {
        const target = document.getElementById(id);
        const frame = document.createElement('iframe');
        frame.title = 'Test ONLYOFFICE';
        target.replaceWith(frame);
        window.editorTestEvents = config.events;
        setTimeout(() => config.events.onAppReady(), 0);
        this.destroyEditor = () => {
          const replacement = document.createElement('div');
          replacement.id = id;
          frame.replaceWith(replacement);
        };
      }};
    ` })
  })
  return crashes
}

for (const ext of ['pdf', 'docx']) {
  test(`${ext}: menu and error recovery survive the editor replacing its target`, async ({ page }) => {
    const crashes = await mockOffice(page)
    await page.goto(`/edit/2?path=18/Example.${ext}`)
    await expect(page.getByTitle('Test ONLYOFFICE', { exact: true })).toBeVisible()
    await expect(page.locator('.editor-overlay')).toHaveCount(0)
    await page.getByRole('button', { name: 'More', exact: true }).click()
    await expect(page.getByRole('link', { name: 'Download original' })).toBeVisible()
    await page.getByRole('button', { name: /Download as/ }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.getByRole('button', { name: 'Close', exact: true }).click()
    await page.evaluate(() => (window as any).editorTestEvents.onError({ data: { errorCode: -4, errorDescription: 'Test download failure' } }))
    await expect(page.locator('.editor-overlay')).toContainText('Test download failure')
    await expect(page.getByText('Something went wrong', { exact: true })).toHaveCount(0)
    await page.getByRole('link', { name: 'Back to files', exact: true }).last().click()
    await expect(page.locator('.editor-page')).toHaveCount(0)
    await expect(page.getByText('This folder is empty', { exact: true })).toBeVisible()
    expect(crashes).toEqual([])
  })
}

test('menu works with an older server response lacking engines', async ({ page }) => {
  const crashes = await mockOffice(page, { legacy: true })
  await page.goto('/edit/2?path=18/Example.docx')
  await expect(page.locator('.editor-overlay')).toHaveCount(0)
  await page.getByRole('button', { name: 'More', exact: true }).click()
  await expect(page.getByRole('link', { name: 'Download original' })).toBeVisible()
  expect(crashes).toEqual([])
})

test('switching engines and returning mounts just one editor', async ({ page }) => {
  const crashes = await mockOffice(page, { delayed: true })
  await page.goto('/edit/2?path=18/Example.docx')
  await expect(page.getByTitle('Test ONLYOFFICE', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'More', exact: true }).click()
  await page.getByRole('button', { name: 'Open with Collabora', exact: true }).click()
  await expect(page.getByTitle('Collabora Online', { exact: true })).toBeVisible()
  await expect(page.getByTitle('Test ONLYOFFICE', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'More', exact: true }).click()
  await page.getByRole('button', { name: 'Open with ONLYOFFICE', exact: true }).click()
  await expect(page.getByTitle('Test ONLYOFFICE', { exact: true })).toBeVisible()
  await expect(page.locator('.editor-host iframe')).toHaveCount(1)
  await expect(page.locator('.editor-overlay')).toHaveCount(0)
  expect(crashes).toEqual([])
})
