import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { defineConfig, globalIgnores } from 'eslint/config'

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
      'no-unused-vars': [
        'error',
        { varsIgnorePattern: '^[A-Z_]', argsIgnorePattern: '^[A-Z_]' },
      ],
    },
  },
  {
    // Node-context files: scripts/, playwright.config.js, e2e/ and the
    // top-level tooling configs (vite/tailwind/postcss/eslint) run under
    // Node (the build tooling / test runner), not the browser.
    files: [
      'scripts/**/*.{js,mjs}',
      'playwright.config.js',
      'e2e/**/*.{js,jsx}',
      '*.config.js',
      'eslint.config.js',
    ],
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
