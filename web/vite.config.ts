import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    // Lightning CSS rewrites max-width queries to range syntax that older mobile browsers ignore.
    cssMinify: false,
  },
  test: {
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    restoreMocks: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8085',
      '/healthz': 'http://127.0.0.1:8085',
    },
  },
})
