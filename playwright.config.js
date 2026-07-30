import { defineConfig, devices } from '@playwright/test'

/**
 * Pango end-to-end tests.
 *
 * Every spec is meant to boot the real Go binary (`./backend/pango --demo`)
 * against an ephemeral in-memory database, so there is no global `webServer`
 * here — see e2e/helpers/node.js. That mirrors flowstock's e2e harness.
 *
 * STATUS: the embed_frontend build lands the React app in the compiled
 * binary (app_embed.go + main.go's "/" route), so specs run rather than
 * skip. `npx playwright test` is 4/5 green; the one failure is a real
 * product gap (see jobs-board.spec.js's inline note), not a harness issue.
 */
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.js',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI ? [['github'], ['list']] : [['list']],
  timeout: 60_000,
  expect: { timeout: 10_000 },
  use: {
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    // Each test navigates to its own node's origin, so no global baseURL.
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
