import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Runs an async fetcher on mount and whenever `deps` changes. Returns
 * { data, error, loading, reload, setData }. Every list/detail page in the app
 * uses this so "loading / error / empty / data" is handled the same way
 * everywhere rather than once per page.
 *
 * `deps` is a value the *caller* builds, so it can never be handed to a hook's
 * dependency list — those have to be array literals for the linter and the
 * React compiler to check them at all, which is why the old
 * `useCallback(run, deps)` could only be silenced, not satisfied. It is
 * serialised to a single scalar here instead, and that scalar is the literal
 * dependency. Every call site passes primitives (ids, filter strings,
 * booleans), which is what makes the serialisation sound.
 */
interface Settled<T> {
  token: string | null
  data: T | null
  error: Error | null
}

// Named so a page can type a query object it passes down as a prop (e.g.
// InspectionDetailPage handing its comparison query to <ComparisonCard>)
// instead of re-deriving the shape with ReturnType<typeof useAsync<...>>.
export interface UseAsyncResult<T> {
  data: T | null
  error: Error | null
  loading: boolean
  reload: () => void
  setData: (next: T | ((prev: T | null) => T)) => void
}

export function useAsync<T>(fetcher: () => Promise<T>, deps: unknown[] = []): UseAsyncResult<T> {
  const depsKey = JSON.stringify(deps)
  // Bumped by reload() so a refetch with unchanged deps still re-runs.
  const [nonce, setNonce] = useState(0)
  // The outcome of a fetch, stamped with the request it answered.
  const [settled, setSettled] = useState<Settled<T>>({ token: null, data: null, error: null })

  const token = `${nonce} ${depsKey}`

  // Latest-ref: `fetcher` is a fresh closure every render, so it cannot be an
  // effect dependency without refetching on every render. Written from an
  // effect rather than during render (refs are not render-time state), and
  // declared before the fetching effect so it is already up to date when that
  // one runs in the same commit.
  const fetcherRef = useRef(fetcher)
  useEffect(() => {
    fetcherRef.current = fetcher
  })

  useEffect(() => {
    let cancelled = false
    fetcherRef.current().then(
      (result) => {
        if (!cancelled) setSettled({ token, data: result, error: null })
      },
      (err: Error) => {
        if (!cancelled) setSettled({ token, data: null, error: err })
      },
    )
    return () => {
      cancelled = true
    }
  }, [token])

  const reload = useCallback(() => setNonce((n) => n + 1), [])

  const setData = useCallback((next: T | ((prev: T | null) => T)) => {
    setSettled((s) => ({ ...s, data: typeof next === 'function' ? (next as (prev: T | null) => T)(s.data) : next }))
  }, [])

  // `loading` is derived from "has the in-flight request answered yet", so
  // entering the loading state costs no setState and no extra render pass.
  // `data` deliberately survives a reload (the previous page of results stays
  // on screen until the next one lands) while `error` does not.
  const current = settled.token === token
  return {
    data: settled.data,
    error: current ? settled.error : null,
    loading: !current,
    reload,
    setData,
  }
}
