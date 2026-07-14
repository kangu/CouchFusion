import { expect, test } from '@playwright/test'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'

test('controls an explicit CouchFusion workspace from one local UI', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  const title = page.getByRole('heading', { name: 'Workspaces' })
  await expect(title).toBeVisible()
  expect(await title.evaluate((element) => Number.parseFloat(getComputedStyle(element).fontSize))).toBeLessThanOrEqual(64)

  await page.setViewportSize({ width: 390, height: 844 })
  expect(await title.evaluate((element) => Number.parseFloat(getComputedStyle(element).fontSize))).toBeLessThanOrEqual(48)
  await page.setViewportSize({ width: 1280, height: 720 })

  await page.getByRole('button', { name: 'Register existing' }).click()
  await page.getByLabel('Absolute path').fill('/tmp/couchfusion-e2e-workspace')
  await page.getByLabel('Display name').fill('E2E Workspace')
  await page.getByRole('button', { name: 'Register exact path' }).click()
  await expect(page.getByRole('heading', { name: 'E2E Workspace' })).toBeVisible()

  await page.getByRole('link', { name: 'Sites', exact: true }).click()
  await page.getByRole('button', { name: 'Create site' }).click()
  await page.getByLabel('Site name').fill('catalog')
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.getByRole('checkbox', { name: 'content' }).check()
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Create site' }).click()
  await expect(page.getByRole('heading', { name: 'Activity' })).toBeVisible()
  await expect(page.getByText('site.create', { exact: true })).toBeVisible()

  await page.getByRole('link', { name: 'Sites', exact: true }).click()
  await page.getByRole('link', { name: /Shop/ }).click()
  await page.getByRole('button', { name: 'Run' }).click()
  await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Start' }).click()
  await expect(page.getByRole('link', { name: /127\.0\.0\.1:4310/ })).toBeVisible()
  await page.getByRole('button', { name: 'Stop' }).click()
  await expect(page.getByRole('heading', { name: 'stopped' })).toBeVisible()

  await page.getByRole('link', { name: 'Configuration' }).click()
  await page.getByLabel(/Default project root/).fill('/tmp/couchfusion-e2e-workspace/apps')
  await page.getByRole('button', { name: 'Review changes' }).click()
  await page.getByRole('button', { name: 'Save override' }).click()
  await expect(page.getByRole('status')).toContainText('saved')

  await page.getByRole('link', { name: 'Workspaces' }).click()
  await page.getByRole('button', { name: 'Forget' }).click()
  await expect(page.getByText('Files at')).toBeVisible()
  await page.getByRole('button', { name: 'Forget registration' }).click()
  await expect(page.getByRole('heading', { name: 'Connect the first workspace.' })).toBeVisible()
})
