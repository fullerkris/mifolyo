import { test, expect } from '@playwright/test';

const query = 'café C++';
const search = `/search/exact?${new URLSearchParams({ q: query })}`;

test.beforeEach(async ({ page }) => {
    const response = await page.goto(search);
    expect(response.headers()['x-mifolyo-browser-fixture']).toBe('1');
});

test('query matches are safely highlighted in relevant excerpts with metadata fallbacks', async ({ page }) => {
    const cards = page.locator('.mf-result-card');
    await expect(cards).toHaveCount(6);
    await expect(cards.first().locator('h2 mark')).toHaveText(['CAFÉ', 'C++']);
    await expect(cards.first().locator('.mf-result-excerpt mark')).toHaveText(['café', 'C++']);
    await expect(cards.first().locator('.mf-result-excerpt')).toContainText('A useful café guide explains C++');
    await expect(cards.first().locator('.mf-result-excerpt')).toHaveText(/^… .* …$/);
    await expect(cards.first().locator('h2')).toContainText('<img src=x onerror=window.unsafeMetadata=1>');
    await expect(cards.first().locator('.mf-result-excerpt')).toContainText('<svg onload=window.unsafeMetadata=1>');
    await expect(cards.locator('h2 img, h2 script, .mf-result-excerpt svg')).toHaveCount(0);
    expect(await page.evaluate(() => window.unsafeMetadata)).toBeUndefined();
    await expect(cards.nth(2).locator('h2')).toHaveText('example.test');
    await expect(cards.nth(2).locator('.mf-result-excerpt')).toHaveText('A description about café and C++.');
    await expect(cards.nth(3).locator('h2')).toHaveText('Untitled page');
    await expect(cards.nth(3).locator('a')).toHaveCount(0);
    await expect(cards.nth(4).locator('.mf-result-excerpt')).toHaveText('A relevant café and C++ description.');
    await expect(cards.nth(5).locator('h2')).toHaveText('0');
    await expect(cards.nth(5).locator('.mf-result-excerpt')).toHaveText('0');
    const source = 'https://example.test/caf%C3%A9?topic=c%2B%2B&lang=fr#examples';
    await expect(cards.first().getByRole('link', { name: 'View page' })).toHaveAttribute('href', source);
    await expect(cards.first().locator('[data-discuss-url]')).toHaveAttribute('data-discuss-url', source);
});

test('long metadata expands by keyboard and fits both themes at supported widths', async ({ page, isMobile }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const card = page.locator('.mf-result-card').nth(1);
    for (const width of isMobile ? [320, 768] : [1440]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const theme of ['light', 'dark']) {
            if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
            await card.scrollIntoViewIfNeeded();
            await expect(card.locator('h2')).toHaveText(/…$/);
            await expect(card.locator('.mf-result-path')).toContainText('…');
            const summary = card.locator('summary');
            await summary.focus();
            await page.keyboard.press('Enter');
            await expect(card.locator('.mf-result-full-title')).toBeVisible();
            await expect(card.locator('.mf-result-full-title')).toHaveText('A long café metadata title with useful detail '.repeat(12).trim());
            const fullSource = `https://example.test/${'long-path/'.repeat(20)}?b=two&a=1#section`;
            await expect(card.locator('details a')).toHaveAttribute('href', fullSource);
            await expect(card.locator('details a')).toHaveText(fullSource);
            expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
            await page.screenshot({ path: testInfo.outputPath(`expanded-metadata-${width}-${theme}.png`), animations: 'disabled' });
            await summary.focus();
            await page.keyboard.press('Enter');
            await expect(card.locator('.mf-result-full-title')).toBeHidden();
        }
    }
    expect(errors).toEqual([]);
});

test('highlights, snippets and full metadata work without JavaScript', async ({ browser, baseURL, isMobile }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL, isMobile, hasTouch: isMobile, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    try {
        const page = await context.newPage();
        await page.goto(search);
        await expect(page.locator('.mf-result-card').first().locator('h2 mark')).toHaveText(['CAFÉ', 'C++']);
        await expect(page.locator('.mf-result-card').nth(2).locator('.mf-result-excerpt')).toHaveText('A description about café and C++.');
        await page.getByText('Full title and URL', { exact: true }).click();
        await expect(page.locator('.mf-result-full-title')).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    } finally {
        await context.close();
    }
});
