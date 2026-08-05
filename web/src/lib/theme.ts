import { useCallback, useEffect, useState } from 'react'

const KEY = 'pango.theme'

type ThemePref = 'light' | 'dark' | 'system'
type ResolvedTheme = 'light' | 'dark'

function resolve(pref: ThemePref): ResolvedTheme {
  if (pref === 'system') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return pref === 'dark' ? 'dark' : 'light'
}

export function useTheme() {
  const [pref, setPref] = useState<ThemePref>(() => (localStorage.getItem(KEY) as ThemePref) || 'system')

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', resolve(pref))
    localStorage.setItem(KEY, pref)
  }, [pref])

  useEffect(() => {
    if (pref !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => document.documentElement.setAttribute('data-theme', resolve(pref))
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [pref])

  const cycle = useCallback(() => {
    setPref((p) => (p === 'light' ? 'dark' : p === 'dark' ? 'system' : 'light'))
  }, [])

  return { pref, resolved: resolve(pref), setPref, cycle }
}
