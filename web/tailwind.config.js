/** @type {import('tailwindcss').Config} */
// Pango — instrument-panel feel: sans UI, mono data, dense, confident.
// Tokens live in src/index.css as CSS variables; this file just exposes them
// as Tailwind utilities so app code never reaches for a raw hex.
export default {
  darkMode: ['class', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{js,jsx,ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: 'var(--bg)',
        surface: 'var(--surface)',
        'surface-raised': 'var(--surface-raised)',
        'surface-sunk': 'var(--surface-sunk)',
        ink: 'var(--ink)',
        'ink-muted': 'var(--ink-muted)',
        'ink-faint': 'var(--ink-faint)',
        line: 'var(--line)',
        'line-strong': 'var(--line-strong)',
        accent: 'var(--accent)',
        'accent-hover': 'var(--accent-hover)',
        'accent-press': 'var(--accent-press)',
        'accent-tint': 'var(--accent-tint)',
        'accent-ink': 'var(--accent-ink)',
        good: 'var(--good)',
        'good-bg': 'var(--good-bg)',
        warning: 'var(--warning)',
        'warning-bg': 'var(--warning-bg)',
        serious: 'var(--serious)',
        'serious-bg': 'var(--serious-bg)',
        critical: 'var(--critical)',
        'critical-bg': 'var(--critical-bg)',
        'series-a': 'var(--series-a)',
        'series-b': 'var(--series-b)',
      },
      fontFamily: {
        sans: [
          'InterVariable', 'Inter', 'ui-sans-serif', 'system-ui', 'sans-serif',
        ],
        mono: [
          '"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'monospace',
        ],
      },
      fontSize: {
        '2xs': ['0.6875rem', { lineHeight: '1rem', letterSpacing: '0.01em' }],
        xs: ['0.75rem', { lineHeight: '1.05rem' }],
        sm: ['0.8125rem', { lineHeight: '1.25rem' }],
        base: ['0.875rem', { lineHeight: '1.4rem' }],
        lg: ['1rem', { lineHeight: '1.5rem', letterSpacing: '-0.006em' }],
        xl: ['1.125rem', { lineHeight: '1.6rem', letterSpacing: '-0.01em' }],
        '2xl': ['1.5rem', { lineHeight: '1.85rem', letterSpacing: '-0.014em' }],
        '3xl': ['2rem', { lineHeight: '2.3rem', letterSpacing: '-0.018em' }],
      },
      boxShadow: {
        e1: '0 1px 2px rgba(0,0,0,0.06), 0 1px 1px rgba(0,0,0,0.04)',
        e2: '0 4px 10px rgba(0,0,0,0.10), 0 1px 2px rgba(0,0,0,0.06)',
        focus: '0 0 0 2px var(--accent-tint), 0 0 0 4px var(--accent)',
      },
      borderRadius: {
        xs: '4px',
        sm: '6px',
        md: '8px',
        lg: '12px',
      },
      keyframes: {
        'fade-in': { '0%': { opacity: 0 }, '100%': { opacity: 1 } },
        'rise-in': {
          '0%': { opacity: 0, transform: 'translateY(4px)' },
          '100%': { opacity: 1, transform: 'translateY(0)' },
        },
      },
      animation: {
        'fade-in': 'fade-in 160ms ease-out both',
        'rise-in': 'rise-in 200ms cubic-bezier(0.22,1,0.36,1) both',
      },
    },
  },
  plugins: [],
}
