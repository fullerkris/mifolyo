import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, sourceURL, waitUntilReady } from './helpers.js';

const root = fileURLToPath(new URL('../../../../', import.meta.url));
const compose = (...args) => execFileSync('docker', [
    'compose', '--project-name', 'mifolyo-search-ui-staging',
    '--file', 'scripts/docker/search-ui-staging.compose.yml', ...args,
], { cwd: root, timeout: 30000, stdio: 'pipe' });

test.beforeEach(async ({ request }) => assertStaging(request));

test('forum outage shows retry and recovers against the real service', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        compose('stop', '--timeout', '5', 'forum-engine');
        stopped = true;
        await page.goto('/search?q=staging');
        await expect(page.locator('.mf-result-card')).toHaveCount(20);
        await expect(page.locator('[data-discussion-count]').first()).toHaveText('—', { timeout: 15000 });
        await expect(page.locator('[data-counts-status]')).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('counts-unavailable.png') });
        await page.locator('[data-discuss-url]').first().click();
        await expect(page.locator('[data-thread-status]')).toContainText('unavailable', { timeout: 15000 });
        await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('forum-unavailable.png') });
        compose('start', 'forum-engine');
        await waitUntilReady(request, `${forumURL}/api/health/ready`);
        stopped = false;
        await page.getByRole('button', { name: 'Try again' }).click();
        await expect(page.locator('.mf-thread')).toHaveCount(20);
        await page.getByRole('button', { name: 'Close discussions' }).click();
        await expect(page.locator('[data-discussion-count]').first()).toHaveText('23');
        await page.getByRole('button', { name: 'Retry counts' }).click();
        await expect(page.locator('[data-discussion-count]').nth(1)).toHaveText('0');
        await expect(page.locator('[data-counts-status]')).toBeHidden();
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
        const webRetry = await page.getByRole('link', { name: 'Try again', exact: true }).getAttribute('href');
        const images = await page.goto('/images?q=ux06&page=2');
        expect(images.status()).toBe(503);
        await expect(page.getByRole('searchbox', { name: 'Search images' })).toHaveValue('ux06');
        const imageRetry = await page.getByRole('link', { name: 'Try again', exact: true }).getAttribute('href');
        await page.screenshot({ path: testInfo.outputPath('images-unavailable.png') });
        const links = await page.goto(`/page-links?${new URLSearchParams({ url: sourceURL, direction: 'incoming', page: '2', from: '/search/exact?q=staging' })}`);
        expect(links.status()).toBe(503);
        await expect(page.getByRole('textbox', { name: 'Explore a page’s connections' })).toHaveValue(sourceURL);
        await page.screenshot({ path: testInfo.outputPath('page-links-unavailable.png') });
        compose('start', 'mongo');
        await waitUntilReady(request, '/api/health/ready');
        stopped = false;
        await page.getByRole('link', { name: 'Try again', exact: true }).click();
        await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
        expect(new URL(page.url()).searchParams.get('direction')).toBe('incoming');
        expect(new URL(page.url()).searchParams.get('from')).toBe('/search/exact?q=staging');
        await page.goto(imageRetry);
        await expect(page.locator('.mf-image-card')).toHaveCount(5);
        await page.goto(webRetry);
        await expect(page.locator('.mf-result-card')).toHaveCount(20);
    } finally {
        if (stopped) {
            compose('start', 'mongo');
            await waitUntilReady(request, '/api/health/ready');
        }
    }
});
