import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

// The same `varsIgnorePattern`/`argsIgnorePattern` exemption applies to the
// plain-JS block (scripts/) and both TS blocks (src/, e2e/) — see the
// comment above the JS block's rule for why it exists.
const jsxUnusedVarsExemption = { varsIgnorePattern: '^[A-Z_]', argsIgnorePattern: '^[A-Z_]' }

export default defineConfig([
  globalIgnores(['dist', 'site/assets/vendor/**', 'backend/**']),
  {
    files: ['**/*.{js,jsx}'],
    extends: [js.configs.recommended, reactHooks.configs.flat.recommended, reactRefresh.configs.vite],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        ecmaVersion: 'latest',
        ecmaFeatures: { jsx: true },
        sourceType: 'module',
      },
    },
    rules: {
      // Core `no-unused-vars` has no JSX awareness — that is what
      // eslint-plugin-react's `jsx-uses-vars` exists for, and this config
      // deliberately carries only the hooks + react-refresh plugins. So any
      // component identifier referenced *only* as `<Foo />` reads as unused.
      // `varsIgnorePattern` already covers the import/declaration case (every
      // `<PlusIcon />`-style import in src/ relies on it). Destructured
      // parameters are governed by `args*`, not `vars*`, so they need the same
      // exemption or e.g. `NAV.map(({ icon: Icon }) => <Icon />)` and
      // `Card({ as: Comp })` are false positives. In this codebase a
      // capitalised identifier is a component or a class, never a plain value.
      'no-unused-vars': ['error', jsxUnusedVarsExemption],
    },
  },
  {
    // src/ is TypeScript. `tseslint.configs.recommended` swaps in the
    // typescript-eslint parser (so type annotations/generics/JSX-in-TSX all
    // parse) and its own type-aware-but-not-project-wide rule set; it also
    // turns the core `no-unused-vars` off in favour of the
    // `@typescript-eslint` version, which needs the same exemption as the JS
    // block above for the same JSX-identifier reason.
    files: ['src/**/*.{ts,tsx}'],
    extends: [tseslint.configs.recommended, reactHooks.configs.flat.recommended, reactRefresh.configs.vite],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', jsxUnusedVarsExemption],
    },
  },
  {
    // The E2E suite (e2e/**), migrated to TypeScript. Mirrors the src block's
    // TS-aware extends/parser rather than being syntax-only. It runs in Node
    // (the Playwright test runner and global-setup.ts's build step) but also
    // authors inline page.evaluate-style callbacks that execute in the page,
    // so both global sets are legitimate.
    files: ['e2e/**/*.{ts,tsx}'],
    extends: [tseslint.configs.recommended, reactHooks.configs.flat.recommended, reactRefresh.configs.vite],
    languageOptions: {
      globals: { ...globals.node, ...globals.browser },
    },
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', jsxUnusedVarsExemption],
      // Playwright's fixture API is `async ({ page }, use) => { await use(x) }`.
      // React also has a `use` hook, so rules-of-hooks sees the call and
      // demands the enclosing function be a component/hook. It is neither —
      // this is a test fixture, not React.
      'react-hooks/rules-of-hooks': 'off',
    },
  },
  {
    // Node-context files: scripts/, playwright.config.js and the top-level
    // tooling configs (vite/tailwind/postcss/eslint) run under Node (the
    // build tooling / test runner), not the browser.
    files: ['scripts/**/*.{js,mjs}', 'playwright.config.js', '*.config.js', 'eslint.config.js'],
    languageOptions: {
      globals: { ...globals.node, ...globals.browser },
    },
    rules: {
      // Playwright's fixture API is `async ({ page }, use) => { await use(x) }`.
      // React also has a `use` hook, so rules-of-hooks sees the call and
      // demands the enclosing function be a component/hook. It is neither.
      'react-hooks/rules-of-hooks': 'off',
    },
  },
])
