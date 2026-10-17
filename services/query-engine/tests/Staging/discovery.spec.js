import { test, expect } from '@playwright/test';
import { assertStaging, navigate, sourceURL } from './helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

test('indexed image associations paginate and return through discussions to exact web results', async ({ page }, testInfo) => {
    const query = 'staging & café C++';
    await navigate(page, () => page.goto(`/search/exact?${new URLSearchParams({ q: query, page: '2' })}`));
    await navigate(page, () => page.getByRole('navigation', { name: 'Search type' }).getByRole('link', { name: 'Images' }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(20);
    await expect(page.getByRole('searchbox', { name: 'Search images' })).toHaveValue(query);
    const first = page.locator('.mf-image-card').first();
    await expect(first.getByRole('link', { name: 'View source' })).toHaveAttribute('href', sourceURL);
    await expect.poll(() => first.locator('img').evaluate(image => image.naturalWidth)).toBe(960);
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.screenshot({ path: testInfo.outputPath(`real-images-${theme}.png`), animations: 'disabled' });
    }
    await navigate(page, () => page.getByRole('link', { name: 'Next →', exact: true }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    const broken = page.locator('.mf-image-card').nth(3);
    await broken.scrollIntoViewIfNeeded();
    await expect(broken.locator('img')).toBeHidden();
    await expect(broken.getByText('Preview unavailable')).toBeVisible();
    await navigate(page, () => first.getByRole('link', { name: 'Discuss source' }).click());
    await expect(page.locator('[data-thread-status]')).not.toContainText('Loading');
    await navigate(page, () => page.getByRole('link', { name: 'Back to results' }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    await navigate(page, () => page.getByRole('navigation', { name: 'Search type' }).getByRole('link', { name: 'Web', exact: true }).click());
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    expect(new URL(page.url()).searchParams.get('q')).toBe(query);
});

test('real incoming and outgoing links retain source navigation and render the bundled map', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await navigate(page, () => page.goto('/search/exact?q=staging'));
    await navigate(page, () => page.locator('.mf-result-card').first().getByRole('link', { name: 'Page links' }).click());
    await expect(page.getByRole('textbox', { name: 'Explore a page’s connections' })).toHaveValue(sourceURL);
    await expect(page.locator('.mf-connection-list li')).toHaveCount(20);
    await expect(page.getByRole('link', { name: /Outgoing links 25/ })).toHaveAttribute('aria-current', 'page');
    await navigate(page, () => page.getByRole('link', { name: 'Next →', exact: true }).click());
    await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
    expect(new URL(page.url()).searchParams.get('url')).toBe(sourceURL);
    await navigate(page, () => page.getByRole('link', { name: /Incoming links 25/ }).click());
    await expect(page.locator('.mf-connection-list li')).toHaveCount(20);
    expect(new URL(page.url()).searchParams.has('page')).toBe(false);
    await navigate(page, () => page.getByRole('link', { name: 'Next →', exact: true }).click());
    await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
    await page.locator('[data-connection-map] summary').click();
    await expect(page.locator('[data-map-canvas] canvas').first()).toBeVisible();
    await expect(page.locator('[data-map-selected-link]')).toHaveAttribute('href', sourceURL);
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.locator('[data-connection-map]').screenshot({ path: testInfo.outputPath(`real-map-${theme}.png`), animations: 'disabled' });
    }
    await navigate(page, () => page.getByRole('link', { name: 'Discuss source', exact: true }).click());
    await expect(page.locator('.mf-thread')).toHaveCount(20);
    await navigate(page, () => page.locator('.mf-thread-open').first().click());
    await expect(page.locator('.mf-thread-body').filter({ hasText: 'seeded local staging conversation' })).toBeVisible();
    await navigate(page, () => page.getByRole('link', { name: 'Back to results' }).click());
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(errors).toEqual([]);
});

test('secondary views have real empty states and work without JavaScript', async ({ browser, baseURL, isMobile }) => {
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    const page = await context.newPage();
    await navigate(page, () => page.goto('/images?q=ux06-no-matches'));
    await expect(page.getByRole('heading', { name: 'No images found' })).toBeVisible();
    await navigate(page, () => page.goto('/images?q=ux06&page=100'));
    await expect(page.getByRole('heading', { name: 'No images on this page' })).toBeVisible();
    await navigate(page, () => page.getByRole('link', { name: 'Back to first page' }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(20);
    await navigate(page, () => page.getByRole('link', { name: 'Next →', exact: true }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    const from = '/images?q=ux06&page=2';
    await navigate(page, () => page.goto(`/page-links?${new URLSearchParams({ url: 'https://example.test/ux06/unknown', from })}`));
    await expect(page.getByRole('heading', { name: 'No outgoing links indexed' })).toBeVisible();
    await page.getByRole('textbox', { name: 'Explore a page’s connections' }).fill(sourceURL);
    await navigate(page, () => page.getByRole('button', { name: 'Explore links', exact: true }).click());
    await expect(page.locator('.mf-connection-list li')).toHaveCount(20);
    await navigate(page, () => page.getByRole('link', { name: 'Next →', exact: true }).click());
    await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
    await page.locator('[data-connection-map] summary').click();
    await expect(page.locator('[data-map-status]')).toContainText('interactive map uses JavaScript');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await navigate(page, () => page.getByRole('link', { name: 'Back to results' }).click());
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    await context.close();
});
