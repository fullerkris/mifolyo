import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, waitUntilReady } from './helpers.js';

const root = fileURLToPath(new URL('../../../../', import.meta.url));
const compose = (...args) => execFileSync('docker', [
    'compose', '--project-name', 'mifolyo-search-ui-staging',
    '--file', 'scripts/docker/search-ui-staging.compose.yml', ...args,
], { cwd: root, timeout: 30000, stdio: 'pipe' });

test.beforeEach(async ({ request }) => assertStaging(request));

test('forum outage shows retry and recovers against the real service', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        await page.goto('/search?q=staging');
        compose('stop', '--timeout', '5', 'forum-engine');
        stopped = true;
        await page.locator('[data-discuss-url]').first().click();
        await expect(page.locator('[data-thread-status]')).toContainText('unavailable', { timeout: 15000 });
        await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('forum-unavailable.png') });
        compose('start', 'forum-engine');
        await waitUntilReady(request, `${forumURL}/api/health/ready`);
        stopped = false;
        await page.getByRole('button', { name: 'Try again' }).click();
        await expect(page.locator('.mf-thread')).toHaveCount(20);
    } finally {
        if (stopped) {
            compose('start', 'forum-engine');
            await waitUntilReady(request, `${forumURL}/api/health/ready`);
        }
    }
});

test('index outage preserves the query and the retry link recovers', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        compose('stop', '--timeout', '5', 'mongo');
        stopped = true;
        const response = await page.goto('/search/exact?q=staging');
        expect(response.status()).toBe(503);
        await expect(page.getByRole('heading', { name: 'Search is temporarily unavailable' })).toBeVisible();
        await expect(page.getByRole('searchbox')).toHaveValue('staging');
        await page.screenshot({ path: testInfo.outputPath('index-unavailable.png') });
        compose('start', 'mongo');
        await waitUntilReady(request, '/api/health/ready');
        stopped = false;
        await page.getByRole('link', { name: 'Try again', exact: true }).click();
        await expect(page.locator('.mf-result-card')).toHaveCount(20);
    } finally {
        if (stopped) {
            compose('start', 'mongo');
            await waitUntilReady(request, '/api/health/ready');
        }
    }
});
