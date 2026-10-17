import { test, expect } from '@playwright/test';

const overflow = async page => expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

test.beforeEach(async ({ page }) => {
    const response = await page.goto('/');
    expect(response.headers()['x-mifolyo-browser-fixture']).toBe('1');
});

test('image pagination and discussion return keep the exact web-search origin', async ({ page }) => {
    const query = 'C++ & café #1';
    const web = `/search/exact?${new URLSearchParams({ q: query, page: '2' })}`;
    await page.goto(web);
    await page.getByRole('navigation', { name: 'Search type' }).getByRole('link', { name: 'Images' }).click();
    await expect(page.locator('.mf-image-card')).toHaveCount(20);
    await expect(page.getByRole('searchbox', { name: 'Search images' })).toHaveValue(query);
    await page.getByRole('link', { name: 'Next →', exact: true }).click();
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    expect(new URL(page.url()).searchParams.get('q')).toBe(query);
    await page.reload();
    const image = page.locator('.mf-image-card').first();
    const source = 'https://example.test/guide/21?ref=search&lang=en';
    await expect(image.getByRole('link', { name: 'View source' })).toHaveAttribute('href', source);
    await image.getByRole('link', { name: 'Discuss source' }).click();
    await expect(page.locator('.mf-thread')).toHaveCount(2);
    expect(new URL(page.url()).searchParams.get('url')).toBe(source);
    await page.locator('.mf-thread-open').first().click();
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await page.getByRole('navigation', { name: 'Search type' }).getByRole('link', { name: 'Web', exact: true }).click();
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await expect(page.getByRole('searchbox')).toHaveValue(query);
});

test('page links paginate, keep their source, and offer a theme-aware keyboard-opened map', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('/search/exact?q=community&page=2');
    await page.locator('.mf-result-card').first().getByRole('link', { name: 'Page links' }).click();
    const source = 'https://example.test/guide/21?ref=search&lang=en';
    await expect(page.getByRole('textbox', { name: 'Explore a page’s connections' })).toHaveValue(source);
    await expect(page.locator('.mf-connection-list li')).toHaveCount(20);
    await expect(page.locator('.mf-page-source em')).toHaveCount(0);
    await page.getByRole('link', { name: 'Next →', exact: true }).click();
    await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
    expect(new URL(page.url()).searchParams.get('url')).toBe(source);
    const summary = page.locator('[data-connection-map] summary');
    await summary.focus();
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-map-canvas] canvas').first()).toBeVisible();
    await expect(page.locator('[data-map-selected-link]')).toHaveAttribute('href', source);
    await page.getByRole('button', { name: 'Zoom in', exact: true }).click();
    await page.getByRole('button', { name: 'Fit map', exact: true }).click();
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        await overflow(page);
        await page.locator('[data-connection-map]').screenshot({ path: testInfo.outputPath(`map-${theme}.png`), animations: 'disabled' });
    }
    await page.getByRole('link', { name: /Incoming links 2/ }).click();
    await expect(page.locator('.mf-connection-list li')).toHaveCount(2);
    expect(new URL(page.url()).searchParams.has('page')).toBe(false);
    await page.locator('.mf-connection-list li').first().getByRole('link', { name: /Explore links/ }).click();
    expect(new URL(page.url()).searchParams.get('url')).toBe('https://example.test/linked/1?b=2&a=1#part');
    await page.getByRole('link', { name: 'Back to results' }).click();
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    expect(errors).toEqual([]);
});

test('secondary empty, invalid, unavailable and throttled states remain actionable', async ({ page }) => {
    for (const [url, heading] of [
        ['/images', 'Start with an image search'], ['/images?q=no-matches', 'No images found'],
        ['/images?q=community&page=3', 'No images on this page'],
        ['/page-links', 'Every page is part of a bigger picture.'],
        ['/page-links?url=https://example.test/empty', 'No outgoing links indexed'],
        ['/page-links?url=https://example.test/source&page=3', 'No links on this page'],
    ]) {
        await page.goto(url);
        await expect(page.getByRole('heading', { name: heading, exact: true })).toBeVisible();
        await overflow(page);
    }
    for (const [url, status] of [
        ['/images?q[]=bad', 422], ['/page-links?url=ftp://example.test', 422],
        ['/images?q=unavailable', 503], ['/page-links?url=https://example.test/unavailable&direction=incoming&page=2', 503],
        ['/images?q=rate-limited', 429], ['/page-links?url=https://example.test/rate-limited', 429],
    ]) {
        const response = await page.goto(url);
        expect(response.status()).toBe(status);
        await expect(page.getByRole('alert')).toBeVisible();
        if (status !== 422) {
            const retry = new URL(await page.getByRole('link', { name: 'Try again' }).getAttribute('href'));
            expect(retry.pathname).toBe(new URL(page.url()).pathname);
        }
    }
});

test('image previews tolerate broken and unsafe metadata in both themes', async ({ page }, testInfo) => {
    await page.goto('/images?q=metadata');
    const cards = page.locator('.mf-image-card');
    await expect(cards.first().locator('h2')).toHaveText('<img src=x onerror=window.unsafeMetadata=1>');
    await expect(cards.first().locator('h2 img')).toHaveCount(0);
    await expect(cards.nth(1).getByText('Source page unavailable')).toBeVisible();
    await expect(cards.nth(1).locator('a, img')).toHaveCount(0);
    await cards.nth(2).scrollIntoViewIfNeeded();
    await expect(cards.nth(2).locator('img')).toBeHidden();
    await expect(cards.nth(2).getByText('Preview unavailable')).toBeVisible();
    expect(await page.evaluate(() => window.unsafeMetadata)).toBeUndefined();
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        await page.evaluate(() => scrollTo(0, 0));
        await overflow(page);
        await page.screenshot({ path: testInfo.outputPath(`images-${theme}.png`), animations: 'disabled' });
    }
});

test('a failed map download leaves links usable and a reload recovers', async ({ page }) => {
    await page.route('**/cytoscape.esm-*.js', route => route.abort());
    await page.goto('/page-links?url=https://example.test/source&page=2');
    await page.locator('[data-connection-map] summary').click();
    await expect(page.locator('[data-map-status]')).toContainText('The map could not load.');
    await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
    await page.unroute('**/cytoscape.esm-*.js');
    await page.getByRole('button', { name: 'Reload page', exact: true }).click();
    await page.locator('[data-connection-map] summary').click();
    await expect(page.locator('[data-map-canvas] canvas').first()).toBeVisible();
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
});

test('images and page links support pagination and return navigation without JavaScript', async ({ browser, baseURL, isMobile }) => {
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    const page = await context.newPage();
    await page.goto('/images?q=community&from=%2Fsearch%2Fexact%3Fq%3Dcommunity%26page%3D2');
    await page.getByRole('link', { name: 'Next →', exact: true }).click();
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    await page.locator('.mf-image-card').first().getByRole('link', { name: 'Page links' }).click();
    await expect(page.locator('.mf-connection-list li')).toHaveCount(20);
    await page.getByRole('link', { name: /Incoming links 2/ }).click();
    await expect(page.locator('.mf-connection-list li')).toHaveCount(2);
    await page.locator('[data-connection-map] summary').click();
    await expect(page.getByText('The interactive map uses JavaScript.', { exact: false })).toBeVisible();
    await overflow(page);
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.locator('.mf-image-card')).toHaveCount(5);
    await page.getByRole('link', { name: 'Back to results' }).click();
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await context.close();
});
