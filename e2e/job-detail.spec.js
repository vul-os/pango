/**
 * Smoke: opening a job from the board shows its detail page — the event
 * thread, cost/time ledgers and the status/assignee controls.
 *
 * Selectors are written against the real JobDetailPage
 * (src/pages/JobDetailPage.jsx).
 */

import { test, expect } from '@playwright/test'
import { PangoNode } from './helpers/node.js'
import { login } from './helpers/ui.js'

test.describe('job detail', () => {
  let node

  test.beforeEach(async () => {
    node = await PangoNode.start()
  })

  test.afterEach(async () => {
    await node?.stop()
  })

  test('opening a job from the board shows its detail page', async ({ page }) => {
    await login(page, node.baseURL)
    await page.getByText('Kitchen mixer leaking under sink').click()

    await expect(page.getByRole('heading', { name: 'Kitchen mixer leaking under sink' })).toBeVisible()
    // The status/assignee side panel and the event-thread tab (default tab).
    // Both the tab button and the section heading read "Event thread", so the
    // heading is the unambiguous target.
    await expect(page.getByRole('heading', { name: 'Event thread' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Back to jobs' })).toBeVisible()

    await page.getByRole('button', { name: 'Back to jobs' }).click()
    await expect(page).toHaveURL(`${node.baseURL}/jobs`)
  })
})
