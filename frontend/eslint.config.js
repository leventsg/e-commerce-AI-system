import js from '@eslint/js'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**', '*.tsbuildinfo'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['src/**/*.{ts,tsx}', '../tests/**/*.{ts,tsx}', 'vite.config.ts'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
      globals: {
        AbortSignal: 'readonly',
        AbortController: 'readonly',
        AsyncGenerator: 'readonly',
        CustomEvent: 'readonly',
        Event: 'readonly',
        HTMLButtonElement: 'readonly',
        HeadersInit: 'readonly',
        ReadableStream: 'readonly',
        RequestInit: 'readonly',
        Response: 'readonly',
        TextDecoder: 'readonly',
        TextEncoder: 'readonly',
        clearInterval: 'readonly',
        console: 'readonly',
        document: 'readonly',
        fetch: 'readonly',
        localStorage: 'readonly',
        setInterval: 'readonly',
        setTimeout: 'readonly',
        window: 'readonly',
      },
    },
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
    },
  },
)
