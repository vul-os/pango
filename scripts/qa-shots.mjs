#!/usr/bin/env node
/**
 * QA screenshotter — every route x 3 widths x both themes, into a gitignored
 * scratch dir (qa-shots/, see .gitignore). Not part of `make screenshots` /
 * the README gallery; this is for manual visual QA of a change across
 * breakpoints and themes in one pass. Pattern borrowed from slipscan's
 * apps/desktop/scripts/qa-shots.mjs.
 *
 * Usage:
 *   npm run qa-shots
 *   QA_OUT=/tmp/shots npm run qa-shots
 *
 * Same honesty guard as scripts/screenshots.mjs: if the pango binary does
 * not yet serve the app UI, this prints why and exits non-zero without
 * writing anything.
 */

import { chromium } from 'playwright'
import { execSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync, statSync, readdirSync } from 'node:fs'
import { resolve, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(__dirname, '..')
const BIN = join(ROOT, 'backend', 'pango')
const PORT = process.env.PANGO_SCREENSHOT_PORT || 28899
const BASE_URL = `http://127.0.0.1:${PORT}`
const OUT = process.env.QA_OUT || join(ROOT, 'qa-shots')
const WIDTHS = [760, 1100, 1520]
const HEIGHT = 960
const DEMO_EMAIL = 'demo@pango.local'
const DEMO_PASSWORD = 'demopassword'

const ROUTES = [
  { name: 'login', path: '/login', public: true },
  { name: 'jobs-board', path: '/jobs' },
  { name: 'jobs-new', path: '/jobs/new' },
  { name: 'buildings', path: '/buildings' },
  { name: 'inspections', path: '/inspections' },
  { name: 'reports', path: '/reports' },
  { name: 'settings', path: '/settings' },
]

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function health() {
  try {
    const res = await fetch(`${BASE_URL}/api/health`)
    return res.ok ? await res.json() : null
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
  if (existsSync(BIN) && statSync(BIN).mtimeMs >= srcAge) return
  console.log('  building pango (frontend + site embedded)…')
  execSync('npm run build:all', { cwd: ROOT, stdio: 'inherit' })
}

async function ensureServer() {
  const running = await health()
  if (running?.demo) {
    console.log(`  reusing running pango --demo instance at ${BASE_URL}`)
    return async () => {}
  }
  if (running) {
    throw new Error(`something non-demo is already listening on ${BASE_URL} — set PANGO_SCREENSHOT_PORT`)
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
    if (exited !== null) throw new Error(`pango exited early (code ${exited}):\n${logs.join('')}`)
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

async function assertAppIsServed() {
  const res = await fetch(`${BASE_URL}/login`)
  const body = await res.text()
  if (res.ok && /<div id="root">/.test(body)) return
  console.error('\nqa-shots: the pango binary does not serve the app UI yet.')
  console.error(`  GET ${BASE_URL}/login -> ${res.status}, body did not look like the React app shell.`)
  console.error('  See scripts/screenshots.mjs\'s header comment for why. No shots were written.\n')
  throw new Error('app UI not served')
}

async function login(page) {
  await page.goto(`${BASE_URL}/login`, { waitUntil: 'networkidle' })
  await page.getByLabel('Email').fill(DEMO_EMAIL)
  await page.getByLabel('Password').fill(DEMO_PASSWORD)
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.waitForURL(`${BASE_URL}/jobs`)
}

async function run() {
  const teardown = await ensureServer()
  try {
    await assertAppIsServed()
    mkdirSync(OUT, { recursive: true })

    const browser = await chromium.launch({ headless: true })
    let shotCount = 0
    try {
      for (const theme of ['dark', 'light']) {
        for (const width of WIDTHS) {
          const ctx = await browser.newContext({
            viewport: { width, height: HEIGHT },
            deviceScaleFactor: 1,
            colorScheme: theme,
          })
          await ctx.addInitScript((t) => localStorage.setItem('pango.theme', t), theme)
          const page = await ctx.newPage()

          let authed = false
          for (const route of ROUTES) {
            if (!route.public && !authed) {
              await login(page)
              authed = true
            }
            await page.goto(`${BASE_URL}${route.path}`, { waitUntil: 'networkidle' })
            await sleep(250)
            const name = `${route.name}__${width}__${theme}.png`
            await page.screenshot({ path: join(OUT, name) })
            console.log(`  ✓ ${name}`)
            shotCount++
          }
          await ctx.close()
        }
      }
    } finally {
      await browser.close()
    }
    console.log(`\nDone -> ${OUT} (${shotCount} shots)`)
  } finally {
    await teardown()
  }
}

run().catch((err) => {
  console.error('\nqa-shots error:', err.message)
  process.exit(1)
})
