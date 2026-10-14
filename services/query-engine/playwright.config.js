import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.SEARCH_UI_BASE_URL || 'http://127.0.0.1:18765';

export default defineConfig({
    testDir: './tests/Browser',
    testMatch: '*.spec.js',
    fullyParallel: false,
    workers: 1,
    retries: 0,
    use: { baseURL, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
    projects: [
        { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1000 } } },
        { name: 'mobile', use: { ...devices['Pixel 7'] } },
    ],
    webServer: process.env.SEARCH_UI_BASE_URL ? undefined : {
        command: 'php -S 127.0.0.1:18765 -t public tests/Browser/router.php',
        url: baseURL,
        reuseExistingServer: false,
        env: {
            APP_ENV: 'testing',
            APP_DEBUG: 'false',
            APP_KEY: 'base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
            SEARCH_UI_BROWSER_FIXTURES: '1',
            SESSION_DRIVER: 'array',
            CACHE_STORE: 'array',
        },
    },
});
