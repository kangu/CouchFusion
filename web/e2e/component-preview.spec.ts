import { expect, test } from '@playwright/test'
import { readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const launchToken = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
const heroSource = resolve('e2e/fixtures/component-preview/app/components/content/Hero.vue')

test('previews a real site component fullscreen with viewport switching and CSS HMR', async ({ page }) => {
  await page.goto(`/?token=${launchToken}`)
  await page.getByRole('button', { name: 'Register existing' }).click()
  await page.getByLabel('Absolute path').fill('/tmp/couchfusion-e2e-workspace')
  await page.getByLabel('Display name').fill('E2E Workspace')
  await page.getByRole('button', { name: 'Register exact path' }).click()
  await page.getByRole('link', { name: 'Sites', exact: true }).click()
  await page.getByRole('link', { name: /Shop/ }).click()
  await page.waitForURL('**/workspaces/workspace-e2e/sites/shop')
  await expect(page.getByRole('heading', { name: 'Shop' })).toBeVisible()

  const components = page.getByRole('region', { name: 'Local content components' })
  await components.getByRole('button', { name: 'Start preview host' }).click()
  await expect(components.getByRole('button', { name: 'Stop preview host' })).toBeVisible()
  await components.getByRole('button', { name: 'Preview Hero' }).click()

  const dialog = page.getByRole('dialog', { name: 'Preview Hero' })
  await expect(dialog).toBeVisible()
  const shell = page.frameLocator('[data-component-preview-frame]')
  await expect(shell.getByRole('heading', { name: 'Hero' })).toBeVisible()
  const render = shell.frameLocator('[data-render-frame]')
  await expect(render.locator('[data-default-probe]')).toHaveText('Declared preview default')
  await expect(render.locator('[data-style-probe]')).toHaveCSS('color', 'rgb(17, 34, 51)')

  await shell.getByRole('button', { name: 'Mobile preview' }).click()
  await expect.poll(async () => shell.locator('[data-render-frame]').evaluate(
    element => Math.round(element.getBoundingClientRect().width),
  )).toBe(375)

  const original = await readFile(heroSource, 'utf8')
  try {
    await writeFile(heroSource, original.replace('rgb(17, 34, 51)', 'rgb(68, 85, 102)'))
    await expect(render.locator('[data-style-probe]')).toHaveCSS('color', 'rgb(68, 85, 102)')
    await expect(dialog).toBeVisible()
  } finally {
    await writeFile(heroSource, original)
  }

  await page.getByRole('button', { name: 'Close component preview' }).click()
  await expect(dialog).toBeHidden()
  await components.getByRole('button', { name: 'Stop preview host' }).click()
  await expect(components.getByRole('button', { name: 'Start preview host' })).toBeVisible()
})
