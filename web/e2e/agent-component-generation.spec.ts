import { expect, test } from '@playwright/test'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'

test('generates a component through explicit questions, approvals, consent, and retained follow-up', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  await page.getByRole('button', { name: 'Register existing' }).click()
  await page.getByLabel('Absolute path').fill('/tmp/couchfusion-agent-e2e-workspace')
  await page.getByLabel('Display name').fill('Agent E2E Workspace')
  await page.getByRole('button', { name: 'Register exact path' }).click()
  await page.getByRole('link', { name: 'Sites', exact: true }).click()
  await page.getByRole('link', { name: /Shop/ }).click()

  const registry = page.getByRole('region', { name: 'Local content components' })
  const generator = page.getByRole('region', { name: 'Create component with Codex' })
  await generator.getByLabel('Describe the component').fill('Create an editorial testimonial rail with useful defaults')
  await generator.getByRole('button', { name: 'Generate' }).click()

  await expect(generator.getByText('Which visual tone?')).toBeVisible()
  await generator.getByRole('combobox').selectOption({ label: 'Quiet' })
  await generator.getByRole('button', { name: 'Send answer' }).click()

  await expect(generator.getByText('Apply component file changes')).toBeVisible()
  await generator.getByRole('button', { name: 'Approve for this generation' }).click()

  await expect(generator.getByText(/dependency manifest or lockfile changes for this generation only/i)).toBeVisible()
  await expect(generator.getByRole('button', { name: 'Approve for this generation' })).toHaveCount(0)
  await generator.getByRole('button', { name: 'Allow once' }).click()
  await generator.getByRole('button', { name: 'Approve once' }).click()

  await expect(generator.getByRole('link', { name: /View generated component hero/ })).toBeVisible()
  await expect(registry.locator('.component-row--generated')).toContainText('Hero')
  await expect(registry.locator('[data-preview-host-status]')).toContainText('Preview Stopped')
  await expect(generator.getByText('app/components/content/Hero.vue')).toBeVisible()
  await expect(generator.getByText('pre-existing.txt')).toBeVisible()

  await generator.getByLabel('Continue this conversation').fill('Make the spacing more compact')
  await generator.getByRole('button', { name: 'Send follow-up' }).click()
  await expect(generator.getByText('Apply component file changes')).toBeVisible()
  await expect(generator.getByRole('button', { name: 'Approve for this generation' })).toBeVisible()

  await page.reload()
  await expect(generator.getByText('Previous conversations')).toBeVisible()
  await generator.getByRole('button', { name: 'Resume' }).click()
  await expect(generator.getByText('Apply component file changes')).toHaveCount(0)
  await expect(generator.getByRole('button', { name: 'Approve for this generation' })).toHaveCount(0)
})
