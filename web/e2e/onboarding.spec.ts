import { expect, test } from '@playwright/test'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'

test('takes an empty installation through generation, preview, and manual Builder completion', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  await expect(page).toHaveURL(/\/getting-started$/)
  await expect(page.getByRole('heading', { name: 'Connect workspace' })).toBeVisible()

  await page.getByRole('tab', { name: 'Register existing' }).click()
  await page.getByLabel('Absolute path').fill('/tmp/couchfusion-e2e-workspace')
  await page.getByLabel('Display name').fill('E2E Workspace')
  await page.getByRole('button', { name: 'Register exact path', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Create or select site' })).toBeFocused()
  await page.getByRole('button', { name: /Shop/ }).click()

  await expect(page.getByRole('heading', { name: 'Run locally' })).toBeVisible()
  await page.getByRole('button', { name: 'Start dev server' }).click()
  await expect(page.getByRole('heading', { name: 'Create and preview' })).toBeVisible()

  const generator = page.getByRole('region', { name: 'Create component with Codex' })
  await generator.getByLabel('Describe the component').fill('Create an editorial testimonial rail with useful defaults')
  await generator.getByRole('button', { name: 'Generate' }).click()
  await generator.getByRole('combobox').selectOption({ label: 'Quiet' })
  await generator.getByRole('button', { name: 'Send answer' }).click()
  await generator.getByRole('button', { name: 'Approve for this generation' }).click()
  await generator.getByRole('button', { name: 'Allow once' }).click()
  await generator.getByRole('button', { name: 'Approve once' }).click()

  await expect(page.locator('.generated-component').getByText('Hero', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Start preview host' }).click()
  await page.getByRole('button', { name: 'Open fullscreen preview' }).click()
  await expect(page.getByRole('dialog', { name: 'Preview Hero' })).toBeVisible()
  await page.getByRole('button', { name: 'Close component preview' }).click()
  await page.getByRole('button', { name: 'Confirm preview' }).click()

  await expect(page.getByRole('heading', { name: 'Place and verify' })).toBeVisible()
  await page.context().route('http://127.0.0.1:4310/builder/', route => route.fulfill({ status: 200, contentType: 'text/html', body: '<title>Fixture Builder</title>' }))
  const builderOpened = page.waitForEvent('popup')
  await page.getByRole('button', { name: 'Open Content Builder' }).click()
  const builder = await builderOpened
  await expect(builder).toHaveURL(/127\.0\.0\.1:4310\/builder\/$/)
  await builder.close()
  await page.getByLabel(/I saved the homepage/).check()
  await page.getByRole('button', { name: 'Complete getting started' }).click()
  await expect(page.getByText('Your first website is running.')).toBeVisible()

  await page.reload()
  await expect(page).toHaveURL(/\/getting-started$/)
  await expect(page.getByText('Your first website is running.')).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.locator('.milestone-mobile-summary')).toBeVisible()

  const restart = page.getByRole('button', { name: 'Start another journey' }).first()
  page.once('dialog', dialog => dialog.accept())
  await restart.click()
  await page.getByRole('button', { name: 'Pause' }).click()
  await expect(page).toHaveURL(/\/$/)
  await page.reload()
  await expect(page).toHaveURL(/\/$/)
  await page.getByRole('button', { name: 'Menu' }).click()
  await page.getByRole('link', { name: 'Resume setup' }).click()
  await expect(page).toHaveURL(/\/getting-started$/)
})
