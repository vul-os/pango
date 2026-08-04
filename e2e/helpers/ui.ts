/**
 * Shared UI flows for the (currently skipped) browser specs. Selectors are
 * written against the real components as landed under src/ — see
 * src/pages/LoginPage.jsx, src/pages/JobsBoardPage.jsx and
 * src/pages/JobDetailPage.jsx. Nothing here is invented: once the compiled
 * binary serves the app (see each spec's skip reason), these should need
 * little to no adjustment.
 */

import type { Page } from '@playwright/test'

export const DEMO_EMAIL = 'demo@pango.local'
export const DEMO_PASSWORD = 'demopassword'

export interface LoginOptions {
  email?: string
  password?: string
}

/** Log in through the real form and wait for the redirect to /jobs. */
export async function login(
  page: Page,
  baseURL: string,
  { email = DEMO_EMAIL, password = DEMO_PASSWORD }: LoginOptions = {},
): Promise<void> {
  await page.goto(`${baseURL}/login`)
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(`${baseURL}/jobs`)
}
