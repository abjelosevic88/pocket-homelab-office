import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  use: { baseURL: 'http://127.0.0.1:18187', headless: true },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 18187 --strictPort',
    url: 'http://127.0.0.1:18187',
  },
})
