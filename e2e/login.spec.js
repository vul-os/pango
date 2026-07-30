/**
 * Smoke: sign in with the demo credentials and land on the jobs board.
 *
 * The flow is written against the real LoginPage (src/pages/LoginPage.jsx)
 * and demo credentials (backend/cmd/pango/demo.go), and runs against the
 * embedded-app binary built by global-setup.js.
 */

import { test, expect } from '@playwright/test'
import { PangoNode } from './helpers/node.js'
import { login } from './helpers/ui.js'

test.describe('login', () => {
  let node

  test.beforeEach(async () => {
    node = await PangoNode.start()
  })

  test.afterEach(async () => {
    await node?.stop()
  })

  test('demo credentials sign in and land on the jobs board', async ({ page }) => {
    await login(page, node.baseURL)
    await expect(page.getByRole('heading', { name: 'Jobs' })).toBeVisible()
  })

  test('a wrong password shows an inline error and does not navigate', async ({ page }) => {
    await page.goto(`${node.baseURL}/login`)
    await page.getByLabel('Email').fill('demo@pango.local')
    await page.getByLabel('Password').fill('not-the-password')
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page.getByText(/could not sign in|invalid/i)).toBeVisible()
    await expect(page).toHaveURL(`${node.baseURL}/login`)
  })
})
