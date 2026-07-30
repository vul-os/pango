/**
 * Smoke: the jobs board shows the seeded demo jobs and can be filtered.
 *
 * Selectors are written against the real JobsBoardPage
 * (src/pages/JobsBoardPage.jsx) and the demo dataset
 * (backend/cmd/pango/demo.go — "Meridian Property Management", Riverside
 * Court / Harbour View / Oakmead Mews).
 */

import { test, expect } from '@playwright/test'
import { PangoNode } from './helpers/node.js'
import { login } from './helpers/ui.js'

test.describe('jobs board', () => {
  let node

  test.beforeEach(async () => {
    node = await PangoNode.start()
  })

  test.afterEach(async () => {
    await node?.stop()
  })

  test('shows seeded demo jobs after login', async ({ page }) => {
    await login(page, node.baseURL)
    // demo.go seeds this exact job at Riverside Court / Flat 3A.
    await expect(page.getByText('Kitchen mixer leaking under sink')).toBeVisible()
  })

  // KNOWN FAILING (real product gap, not a selector/harness issue): demo.go
  // seeds "Lift service overdue" with status on_hold, and BOARD_COLUMNS
  // (src/lib/domain.js) has no column for on_hold. Its own comment says
  // on_hold "fold[s] into neighbours visually via a badge", but BoardView's
  // `jobs.filter((j) => j.status === col.status)` has no such fold-in — an
  // on_hold job matches no column and never renders on the board at all, so
  // the search box has nothing to find. Fix belongs in BoardView/domain.js,
  // not here.
  test('search filters the board to matching jobs', async ({ page }) => {
    await login(page, node.baseURL)
    await page.getByPlaceholder('Title, description, job #').fill('lift service')
    await expect(page.getByText('Lift service overdue')).toBeVisible()
    await expect(page.getByText('Kitchen mixer leaking under sink')).toHaveCount(0)
  })
})
