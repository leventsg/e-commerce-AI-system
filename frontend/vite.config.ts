import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import { loadEnv, type ProxyOptions } from 'vite'

const defaultTargets = {
  user: 'http://localhost:8001',
  product: 'http://localhost:8002',
  carts: 'http://localhost:8003',
  order: 'http://localhost:8004',
  checkout: 'http://localhost:8005',
  payment: 'http://localhost:8006',
  ai: 'http://localhost:8007',
  coupon: 'http://localhost:8009',
} as const

function targetFor(env: Record<string, string>, service: keyof typeof defaultTargets) {
  return env[`VITE_${service.toUpperCase()}_API_TARGET`] || env.VITE_API_TARGET || defaultTargets[service]
}

function proxyTo(target: string, options: Partial<ProxyOptions> = {}): ProxyOptions {
  return {
    target,
    changeOrigin: true,
    ...options,
  }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, fileURLToPath(new URL('.', import.meta.url)), '')

  return {
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
        '/douyin/user': proxyTo(targetFor(env, 'user')),
        '/douyin/product': proxyTo(targetFor(env, 'product')),
        '/douyin/carts': proxyTo(targetFor(env, 'carts')),
        '/douyin/order': proxyTo(targetFor(env, 'order')),
        '/douyin/checkout': proxyTo(targetFor(env, 'checkout')),
        '/douyin/payment': proxyTo(targetFor(env, 'payment')),
        '/douyin/ai': proxyTo(targetFor(env, 'ai'), { ws: true }),
        '/douyin/coupon': proxyTo(targetFor(env, 'coupon')),
      },
    },
    test: {
      environment: 'jsdom',
      include: ['../tests/**/*.test.ts', '../tests/**/*.test.tsx'],
      setupFiles: [fileURLToPath(new URL('../tests/frontend/setup.ts', import.meta.url))],
      globals: true,
    },
  }
})
