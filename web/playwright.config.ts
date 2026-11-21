import { defineConfig } from '@playwright/test'

export default defineConfig({
  testMatch: 'knowledge-*.spec.ts',
  outputDir: '/tmp/hsg-knowledge-extraction-results',
  use: {
    baseURL: 'http://127.0.0.1:3199',
    launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH, args: ['--no-sandbox'] }
      : {},
  },
  webServer: {
    command: 'corepack pnpm@11.10.0 exec vite --host 127.0.0.1 --port 3199',
    url: 'http://127.0.0.1:3199',
    reuseExistingServer: !process.env.CI,
  },
})
