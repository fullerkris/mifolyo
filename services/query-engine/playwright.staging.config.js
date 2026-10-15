import { defineConfig, devices } from '@playwright/test';

// This suite operates only on the dedicated loopback staging Compose project.
export default defineConfig({
    testDir: './tests/Staging',
    outputDir: './test-results/staging',
    fullyParallel: false,
    workers: 1,
    retries: 0,
    timeout: 60000,
    use: {
        baseURL: 'http://127.0.0.1:18081',
        trace: 'retain-on-failure',
        screenshot: 'only-on-failure',
    },
    projects: [
        { name: 'desktop', testMatch: ['search-ui.spec.js', 'discovery.spec.js', 'voting.spec.js'], use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1000 } } },
        { name: 'mobile', testMatch: ['search-ui.spec.js', 'discovery.spec.js', 'voting.spec.js'], use: { ...devices['Pixel 7'] } },
        { name: 'recovery', testMatch: 'recovery.spec.js', dependencies: ['desktop', 'mobile'], use: { ...devices['Desktop Chrome'] } },
    ],
});
