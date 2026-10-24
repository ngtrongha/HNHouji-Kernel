import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests/browser', workers: 1, retries: 0,
  use: { browserName: 'chromium', viewport: { width: 390, height: 844 }, screenshot: 'only-on-failure' },
  reporter: 'list',
});
