import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@testing-library/react': fileURLToPath(new URL('./node_modules/@testing-library/react/dist/@testing-library/react.esm.js', import.meta.url)),
      '@testing-library/user-event': fileURLToPath(new URL('./node_modules/@testing-library/user-event/dist/esm/index.js', import.meta.url)),
    },
  },
  server: {
    fs: {
      allow: [fileURLToPath(new URL('..', import.meta.url))],
    },
    proxy: {
      '/douyin/user': {
        target: 'http://localhost:8001',
        changeOrigin: true,
      },
      '/douyin/product': {
        target: 'http://localhost:8002',
        changeOrigin: true,
      },
      '/douyin/carts': {
        target: 'http://localhost:8003',
        changeOrigin: true,
      },
      '/douyin/order': {
        target: 'http://localhost:8004',
        changeOrigin: true,
      },
      '/douyin/checkout': {
        target: 'http://localhost:8005',
        changeOrigin: true,
      },
      '/douyin/payment': {
        target: 'http://localhost:8006',
        changeOrigin: true,
      },
      '/douyin/ai': {
        target: 'http://localhost:8007',
        changeOrigin: true,
      },
      '/douyin/coupon': {
        target: 'http://localhost:8009',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    include: ['../tests/**/*.test.ts', '../tests/**/*.test.tsx'],
    setupFiles: [fileURLToPath(new URL('../tests/frontend/setup.ts', import.meta.url))],
    globals: true,
  },
})
