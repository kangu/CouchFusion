import { expect, test } from '@playwright/test'
import { writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'

test('keeps the Go session while Vite hot-reloads CSS', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  const probe = page.locator('#probe')
  await expect(probe).toHaveCSS('color', 'rgb(17, 34, 51)')

  const bootstrap = await page.request.get('/api/v1/bootstrap')
  expect(bootstrap.ok()).toBe(true)

  await writeFile(resolve('output/hmr-fixture/style.css'), '#probe { color: rgb(68, 85, 102); }\n')
  await expect(probe).toHaveCSS('color', 'rgb(68, 85, 102)')

  const afterUpdate = await page.request.get('/api/v1/bootstrap')
  expect(afterUpdate.ok()).toBe(true)
  expect(page.url()).toBe('http://127.0.0.1:4178/')
})
