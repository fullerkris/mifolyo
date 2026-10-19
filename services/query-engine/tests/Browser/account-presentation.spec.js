import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
    const response = await page.goto('/');
    expect(response.headers()['x-mifolyo-browser-fixture']).toBe('1');
});

test('related thumbnail failure has a readable fallback and keyboard-accessible exact source', async ({ page }, info) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto('/search/exact?q=sidebar-preview');
    const sidebar = page.getByRole('region', { name: 'Related images' });
    const broken = sidebar.locator('.mf-related-preview').first();
    await broken.scrollIntoViewIfNeeded();
    await expect(broken.locator('img')).toBeHidden();
    await expect(broken.getByText('Preview unavailable')).toBeVisible();
    await expect(broken.locator('.mf-image-fallback')).toHaveAttribute('aria-hidden', 'false');
    const source = sidebar.getByRole('link', { name: /A captured image source/ });
    await expect(source).toHaveAttribute('href', 'https://example.test/images?b=2&a=1#source');
    await source.focus();
    await expect(source).toBeFocused();
    await expect(source.locator('em')).toHaveCount(0);
    const working = sidebar.locator('.mf-related-preview').nth(1);
    await working.scrollIntoViewIfNeeded();
    await expect.poll(() => working.locator('img').evaluate(image => image.naturalWidth)).toBeGreaterThan(0);
    await expect(working.locator('.mf-image-fallback')).toHaveAttribute('aria-hidden', 'true');
    for (const width of info.project.name === 'mobile' ? [320] : [1440, 768]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const theme of ['light', 'dark']) {
            if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
            expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
            await sidebar.screenshot({ path: info.outputPath(`sidebar-${width}-${theme}.png`), animations: 'disabled' });
        }
    }
    expect(errors).toEqual([]);
});

test('discussion cards, threads and nested replies show author levels and tolerate missing authors', async ({ page, browser, baseURL }, info) => {
    await page.goto('/discussions?url=https%3A%2F%2Fexample.test%2Fsource');
    await expect(page.locator('.mf-thread .mf-author-level')).toHaveText(['Level 4', 'Level 2']);
    await page.locator('.mf-thread-open').first().click();
    await expect(page.locator('.mf-thread-detail .mf-author-level')).toHaveText('Level 4');
    await expect(page.locator('#comment-4 > article .mf-author-level')).toHaveText('Level 4');
    await expect(page.locator('#comment-5 .mf-author-level')).toHaveText('Level 2');
    await expect(page.locator('#comment-6 .mf-author-name')).toHaveText('Community member');
    await expect(page.locator('#comment-6 .mf-author-level, #comment-7 .mf-author-level')).toHaveCount(0);
    for (const width of info.project.name === 'mobile' ? [320] : [1440, 768]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const theme of ['light', 'dark']) {
            if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
            expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
            await page.screenshot({ path: info.outputPath(`authors-${width}-${theme}.png`), fullPage: true, animations: 'disabled' });
        }
    }
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: 320, height: 1000 } });
    try {
        const noScript = await context.newPage();
        await noScript.goto('/threads/1');
        await expect(noScript.locator('.mf-thread-detail .mf-author-level')).toHaveText('Level 4');
        await expect(noScript.locator('#comment-5 .mf-author-level')).toHaveText('Level 2');
    } finally { await context.close(); }
});

test('account recovery entry points preserve the exact discussion destination', async ({ page }) => {
    const returnTo = '/threads/1?from=%2Fimages%3Fq%3Dlibrary%26page%3D2&reply_to=5#comment-5';
    await page.goto('/sign-in?'+new URLSearchParams({ return_to: returnTo }));
    await page.getByRole('link', { name: 'Forgot your password?' }).click();
    expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
    await expect(page.getByLabel('Email', { exact: true })).toBeVisible();
    await page.getByRole('link', { name: 'Back to sign in' }).click();
    expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
    await page.goto('/reset-password');
    await expect(page.getByRole('link', { name: 'Request a new reset link' })).toBeVisible();
    await expect(page.getByLabel('New password', { exact: true })).toHaveCount(0);
});
