/**
 * Pango E2E node harness.
 *
 * Every test is meant to drive the REAL Go binary: a single self-contained
 * `pango` process with the frontend embedded, pointed at a throwaway data
 * dir and a free port. Nothing mocked, nothing shared between tests, so
 * specs can run in parallel. Mirrors flowstock's e2e/helpers/node.js.
 *
 * STATUS: this helper works today (start/stop, health check, HTTP client) —
 * what does not work yet is the app UI it would otherwise let a spec drive.
 * See playwright.config.js and any spec file for why.
 */

import { spawn } from 'child_process'
import { mkdtempSync, rmSync, existsSync } from 'fs'
import { tmpdir } from 'os'
import net from 'net'
import { join, resolve, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
export const ROOT = resolve(__dirname, '..', '..')
export const BIN = process.env.PANGO_BIN || join(ROOT, 'backend', 'pango')

/** Ask the OS for a free port. */
async function freePort() {
  return new Promise((res, rej) => {
    const srv = net.createServer()
    srv.on('error', rej)
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address()
      srv.close(() => res(port))
    })
  })
}

/**
 * A running Pango instance plus a thin API client for it. The client is
 * used for arrange/assert steps that are not the subject of a test (seeding
 * via demo mode, reading back a job); flows under test are driven through
 * the browser once there is a UI to drive.
 */
export class PangoNode {
  constructor({ port, dataDir, proc }) {
    this.port = port
    this.dataDir = dataDir
    this.proc = proc
    this.baseURL = `http://127.0.0.1:${port}`
    this.logs = []
  }

  /** Boot a node on a free port, in --demo mode by default (no disk writes). */
  static async start(opts = {}) {
    if (!existsSync(BIN)) {
      throw new Error(
        `pango binary not found at ${BIN} — run \`npm run build:all\` (global setup does this automatically)`,
      )
    }
    const port = opts.port || (await freePort())
    const demo = opts.demo !== false
    const dataDir = demo ? null : opts.dataDir || mkdtempSync(join(tmpdir(), 'pango-e2e-'))

    const args = ['-addr', `127.0.0.1:${port}`]
    if (demo) {
      args.push('-demo')
    } else {
      args.push('-db', join(dataDir, 'pango.db'))
    }

    const proc = spawn(BIN, args, {
      cwd: dataDir || ROOT,
      env: { ...process.env, ...(opts.env || {}) },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    const node = new PangoNode({ port, dataDir, proc })
    proc.stdout.on('data', (d) => node.logs.push(String(d)))
    proc.stderr.on('data', (d) => node.logs.push(String(d)))
    proc.on('exit', (code) => {
      node.exited = code
    })
    await node.waitReady()
    return node
  }

  async waitReady(timeoutMs = 20000) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      if (this.exited !== undefined) {
        throw new Error(`pango exited early (code ${this.exited}):\n${this.logs.join('')}`)
      }
      try {
        const res = await fetch(`${this.baseURL}/api/health`)
        if (res.ok) return
      } catch {
        /* not up yet */
      }
      await new Promise((r) => setTimeout(r, 50))
    }
    throw new Error(`pango did not become ready on ${this.baseURL}:\n${this.logs.join('')}`)
  }

  async stop() {
    if (this.proc && this.exited === undefined) {
      this.proc.kill('SIGTERM')
      const deadline = Date.now() + 5000
      while (this.exited === undefined && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 25))
      }
      if (this.exited === undefined) this.proc.kill('SIGKILL')
    }
    if (this.dataDir && !process.env.PANGO_KEEP_DATA) {
      rmSync(this.dataDir, { recursive: true, force: true })
    }
  }

  // ── HTTP client ───────────────────────────────────────────────────────────

  async req(method, path, body) {
    const res = await fetch(`${this.baseURL}${path}`, {
      method,
      headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
    const text = await res.text()
    if (!res.ok) {
      throw new Error(`${method} ${path} -> ${res.status}: ${text.trim()}`)
    }
    if (!text) return null
    try {
      return JSON.parse(text)
    } catch {
      return text
    }
  }

  health() {
    return this.req('GET', '/api/health')
  }
}

/** Wait until `fn()` returns truthy, polling. No arbitrary sleeps. */
export async function until(fn, { timeout = 10000, interval = 50, message } = {}) {
  const deadline = Date.now() + timeout
  let last
  for (;;) {
    try {
      last = await fn()
      if (last) return last
    } catch (err) {
      last = err.message
    }
    if (Date.now() > deadline) {
      throw new Error(`timed out waiting for ${message || 'condition'} (last: ${JSON.stringify(last)})`)
    }
    await new Promise((r) => setTimeout(r, interval))
  }
}
