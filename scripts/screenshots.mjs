#!/usr/bin/env node
/**
 * Pango screenshot generator (docs/SCREENSHOTS.md).
 *
 * Captures docs/screenshots/*.png (and mirrors them into site/screenshots/,
 * which is what site/docs.html actually loads — see its image-path rewrite)
 * using Playwright/Chromium at 1440x900, driving the real compiled binary in
 * demo mode (`./backend/pango --demo`) — no database, no config, no
 * credentials, nothing mocked.
 *
 * Usage:
 *   npx playwright install chromium   # one-time
 *   npm run screenshots
 *
 * If a pango --demo instance is already running on the default port, it is
 * reused; otherwise the binary is built (if missing/stale) and spawned, and
 * torn down again when this script exits.
 *
 * HONESTY GUARD: before capturing anything, this script smoke-tests that the
 * binary actually serves the app UI (backend/cmd/pango's app_embed.go +
 * main.go's "/" route). If that ever regresses and `/login` 404s again, this
 * script prints exactly why and exits non-zero WITHOUT writing any file to
 * docs/screenshots/ or site/screenshots/. No placeholder or faked image is
 * ever produced.
 */

import { chromium } from 'playwright'
import { execSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync, copyFileSync, statSync, readdirSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(__dirname, '..')
const BIN = join(ROOT, 'backend', 'pango')
const PORT = process.env.PANGO_SCREENSHOT_PORT || 28899
const BASE_URL = `http://127.0.0.1:${PORT}`
const VIEWPORT = { width: 1440, height: 900 }
const OUT_DIRS = [join(ROOT, 'docs', 'screenshots'), join(ROOT, 'site', 'screenshots')]
const DEMO_EMAIL = 'demo@pango.local'
const DEMO_PASSWORD = 'demopassword'

// Ordered roughly by docs/SCREENSHOTS.md's shot list, using the routes that
// actually exist in src/App.jsx today. "buildings" is the hero (shot #1 in
// that list — "building overview").
const ROUTES = [
  { name: 'jobs-board', hash: '/jobs', hero: true },
  { name: 'job-detail', hash: null }, // resolved at capture time, see below
  { name: 'inspection-comparison', hash: null }, // ditto
  { name: 'inspections', hash: '/inspections' },
  { name: 'reports', hash: '/reports' },
  { name: 'buildings', hash: '/buildings' },
  { name: 'settings', hash: '/settings' },
]

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function health() {
  try {
    const res = await fetch(`${BASE_URL}/api/health`)
    if (!res.ok) return null
    return await res.json()
  } catch {
    return null
  }
}

function newestMtime(path, ignored = new Set(['node_modules', 'dist', '.git', 'pango'])) {
  if (!existsSync(path)) return 0
  const st = statSync(path)
  if (!st.isDirectory()) return st.mtimeMs
  let newest = st.mtimeMs
  for (const entry of readdirSync(path)) {
    if (ignored.has(entry)) continue
    newest = Math.max(newest, newestMtime(join(path, entry), ignored))
  }
  return newest
}

function ensureBinary() {
  const sources = ['src', 'backend', 'index.html', 'package.json', 'vite.config.js'].map((p) => join(ROOT, p))
  const srcAge = Math.max(...sources.map((p) => newestMtime(p)))
  if (existsSync(BIN) && statSync(BIN).mtimeMs >= srcAge) {
    console.log('  reusing up-to-date pango binary')
    return
  }
  console.log('  building pango (frontend + site embedded)…')
  execSync('npm run build:all', { cwd: ROOT, stdio: 'inherit' })
}

/** Reuse a running --demo instance, or build+spawn our own. Returns a teardown fn. */
async function ensureServer() {
  const running = await health()
  if (running?.demo) {
    console.log(`  reusing running pango --demo instance at ${BASE_URL}`)
    return async () => {}
  }
  if (running) {
    throw new Error(
      `something is already listening on ${BASE_URL} and it is not a --demo instance (demo=${running.demo}) — set PANGO_SCREENSHOT_PORT to use a different port`,
    )
  }

  ensureBinary()

  console.log(`  starting pango --demo on ${BASE_URL}…`)
  const proc = spawn(BIN, ['--demo', '--addr', `127.0.0.1:${PORT}`], {
    cwd: ROOT,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  const logs = []
  proc.stdout.on('data', (d) => logs.push(String(d)))
  proc.stderr.on('data', (d) => logs.push(String(d)))
  let exited = null
  proc.on('exit', (code) => {
    exited = code
  })

  const deadline = Date.now() + 20_000
  while (Date.now() < deadline) {
    if (exited !== null) {
      throw new Error(`pango exited early (code ${exited}):\n${logs.join('')}`)
    }
    if (await health()) break
    await sleep(100)
  }
  if (!(await health())) {
    proc.kill('SIGTERM')
    throw new Error(`pango did not become ready on ${BASE_URL}:\n${logs.join('')}`)
  }

  return async () => {
    proc.kill('SIGTERM')
    const stopDeadline = Date.now() + 5000
    while (exited === null && Date.now() < stopDeadline) await sleep(25)
    if (exited === null) proc.kill('SIGKILL')
  }
}

/** Confirm the binary actually serves the app before touching the filesystem. */
async function assertAppIsServed() {
  const res = await fetch(`${BASE_URL}/login`)
  const body = await res.text()
  const looksLikeTheApp = res.ok && /<div id="root">/.test(body)
  if (looksLikeTheApp) return

  console.error('\nscreenshots: the pango binary does not serve the app UI yet.')
  console.error(`  GET ${BASE_URL}/login -> ${res.status}, body did not look like the React app shell.`)
  console.error(
    '  This is expected until backend/cmd/pango gains a //go:embed for the built app (dist/) and\n' +
      '  main.go registers a "/" route — see the repo furniture report / Makefile "build" target note.',
  )
  console.error('  No screenshots were written.\n')
  throw new Error('app UI not served')
}

async function run() {
  console.log(`\nPango screenshotter`)
  console.log(`  BASE_URL : ${BASE_URL}`)
  console.log(`  output   : ${OUT_DIRS.join(', ')}\n`)

  const teardown = await ensureServer()
  try {
    await assertAppIsServed()

    for (const dir of OUT_DIRS) mkdirSync(dir, { recursive: true })

    const browser = await chromium.launch({ headless: true })
    try {
      for (const theme of ['light', 'dark']) {
        const ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 2 })
        await ctx.addInitScript((t) => localStorage.setItem('pango.theme', t), theme)
        const page = await ctx.newPage()

        await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
        await page.getByLabel('Email').fill(DEMO_EMAIL)
        await page.getByLabel('Password').fill(DEMO_PASSWORD)
        await page.getByRole('button', { name: /sign in/i }).click()
        await page.waitForURL(`${BASE_URL}/jobs`)

        for (const route of ROUTES) {
          let target = route.hash
          if (route.name === 'job-detail') {
            // Follow the first job card from the board rather than hard-coding
            // an id — ids are generated per demo run.
            await page.goto(`${BASE_URL}/jobs`, { waitUntil: 'networkidle' })
            await page.locator('a[href^="/jobs/"]').first().click()
            await page.waitForURL(/\/jobs\/.+/)
          } else if (route.name === 'inspection-comparison') {
            // The ingoing/outgoing diff is the product's differentiator, so it
            // gets its own shot. Follow the outgoing inspection from the list —
            // it is the side that has a baseline to compare against.
            await page.goto(`${BASE_URL}/inspections`, { waitUntil: 'networkidle' })
            const outgoing = page.locator('a[href^="/inspections/"]').filter({ hasText: /outgoing/i }).first()
            const link = (await outgoing.count()) ? outgoing : page.locator('a[href^="/inspections/"]').first()
            await link.click()
            await page.waitForURL(/\/inspections\/.+/)
            await sleep(400) // let the comparison fetch settle
          } else {
            await page.goto(`${BASE_URL}${target}`, { waitUntil: 'networkidle' })
          }
          await page.evaluate(() => document.fonts.ready)
          await sleep(300)

          const suffix = theme === 'dark' ? '-dark' : ''
          const name = `${route.name}${suffix}.png`
          await page.screenshot({ path: join(OUT_DIRS[0], name) })
          console.log(`  ✓ ${name}`)
          if (route.hero && theme === 'light') {
            await page.screenshot({ path: join(OUT_DIRS[0], 'hero.png') })
            console.log(`  ✓ hero.png (copy of ${name})`)
          }
        }
        await ctx.close()
      }
    } finally {
      await browser.close()
    }

    // Mirror everything into site/screenshots/ (site/docs.html loads shots
    // from there — see its image-path rewrite).
    const files = readdirSync(OUT_DIRS[0]).filter((f) => f.endsWith('.png'))
    for (const f of files) copyFileSync(join(OUT_DIRS[0], f), join(OUT_DIRS[1], f))
    console.log(`\nMirrored ${files.length} screenshots into site/screenshots/`)
    console.log('Done.')
  } finally {
    await teardown()
  }
}

run().catch((err) => {
  console.error('\nscreenshotter error:', err.message)
  process.exit(1)
})
