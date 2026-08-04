import userEvent, { type Options } from '@testing-library/user-event'

/**
 * `userEvent`, without the per-keystroke timer it otherwise awaits.
 *
 * userEvent's default (`delay: 0`) still yields to a real `setTimeout` between
 * every key event, so a test that fills four fields pays ~80 trips through the
 * event loop and a React render on each one. That left the slowest form test
 * (RegisterPage's 403 case, 78 keystrokes) running 3.0–4.5s against Vitest's
 * 5s default timeout: green on an idle machine, and timing out inside
 * `scripts/check.sh`, where the frontend suite runs right after the Go build
 * and the box is still busy. A gate whose result depends on machine load is
 * not a gate, so the waste is removed rather than the ceiling raised.
 *
 * `delay: null` dispatches exactly the same events in the same order — no
 * assertion changes, ~6x faster (that test now runs in ~0.6s).
 *
 * If a test needs fake timers, pass an explicit `advanceTimers` here; do not
 * reach for the bare `userEvent.setup()` default again.
 */
export function setupUser(options?: Options) {
  return userEvent.setup({ delay: null, ...options })
}
