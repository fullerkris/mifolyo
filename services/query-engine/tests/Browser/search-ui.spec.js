import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
    const response = await page.goto('/');
    expect(response.headers()['x-mifolyo-browser-fixture']).toBe('1');
});

test('search, pagination, refresh and history preserve the exact query', async ({ page }, testInfo) => {
    const query = 'C++ & café #1';
    await page.getByRole('searchbox').fill(query);
    await page.getByRole('searchbox').press('Enter');
    await expect(page).toHaveURL(/\/search\?/);
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    expect(new URL(page.url()).searchParams.get('q')).toBe(query);
    await page.getByRole('link', { name: 'Next →', exact: true }).click();
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    expect(new URL(page.url()).searchParams.get('q')).toBe(query);
    await page.reload();
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    await page.goBack();
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    await expect(page.locator('[data-navigation-status]')).toBeHidden();
    await page.screenshot({ path: testInfo.outputPath('search-results.png'), fullPage: true });
});

test('theme persists and layouts fit the viewport', async ({ page }, testInfo) => {
    await page.evaluate(() => { localStorage.setItem('mifolyo-theme', 'light'); });
    await page.reload();
    await page.screenshot({ path: testInfo.outputPath('home-light.png'), fullPage: true });
    await page.getByRole('button', { name: 'Switch to dark mode' }).click();
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await page.screenshot({ path: testInfo.outputPath('home-dark.png'), fullPage: true });
    await page.goto('/search?q=community');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('selected source opens real discussion transport on desktop and mobile', async ({ page, isMobile }, testInfo) => {
    await page.goto('/search?q=community');
    const discuss = page.locator('[data-discuss-url]').first();
    const source = await discuss.getAttribute('data-discuss-url');
    const request = page.waitForRequest((request) => request.url().includes('/api/threads/by-url'));
    await discuss.click();
    expect(new URL((await request).url()).searchParams.get('url')).toBe(source);
    await expect(page.getByRole('heading', { name: 'What makes this source useful?' })).toBeVisible();
    if (isMobile) {
        expect(new URL(page.url()).pathname).toBe('/discussions');
        expect(new URL(page.url()).searchParams.get('url')).toBe(source);
    } else {
        const dialog = page.getByRole('dialog');
        await expect(dialog).toBeVisible();
        await page.keyboard.press('Tab');
        expect(await page.evaluate(() => document.querySelector('dialog').contains(document.activeElement))).toBe(true);
        await page.screenshot({ path: testInfo.outputPath('discussion-panel.png') });
        await page.keyboard.press('Escape');
        await expect(dialog).not.toBeVisible();
        await expect(discuss).toBeFocused();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('empty search, service failure and rate limit have useful recovery states', async ({ page }) => {
    await page.goto('/search?q=no-matches');
    await expect(page.getByRole('heading', { name: 'No results found' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Search result pages' })).toHaveCount(0);
    const unavailable = await page.goto('/search?q=unavailable');
    expect(unavailable.status()).toBe(503);
    await expect(page.getByRole('searchbox')).toHaveValue('unavailable');
    await expect(page.getByRole('link', { name: 'Try again' })).toBeVisible();
    const limited = await page.goto('/search?q=rate-limited');
    expect(limited.status()).toBe(429);
    await expect(page.getByRole('heading', { name: 'A moment to catch up' })).toBeVisible();
});

test('discussion loading, retry, empty and malformed responses', async ({ page }) => {
    let release;
    await page.route('**/api/threads/by-url?**', async (route) => {
        await new Promise((resolve) => { release = resolve; });
        await route.fulfill({ status: 503, json: { message: 'Unavailable' } });
    });
    await page.goto('/discussions?url=https%3A%2F%2Fexample.test%2Fsource');
    await expect(page.locator('[data-thread-status]')).toHaveText('Loading community threads…');
    await expect.poll(() => typeof release).toBe('function');
    release();
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
    await page.unroute('**/api/threads/by-url?**');
    await page.route('**/api/threads/by-url?**', (route) => route.fulfill({ json: { data: [], meta: { total: 0 } } }));
    await page.getByRole('button', { name: 'Try again' }).click();
    await expect(page.locator('[data-thread-status]')).toContainText('No discussions yet');
    await page.unroute('**/api/threads/by-url?**');
    await page.route('**/api/threads/by-url?**', (route) => route.fulfill({ json: { data: 'not-a-list' } }));
    await page.getByLabel('Sort').selectOption('new');
    await expect(page.locator('[data-thread-status]')).toContainText('unexpected response');
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
});

test('discussion page and sort requests keep the exact source and escape content', async ({ page }) => {
    const source = 'https://example.test/source?a=1&b=two+words';
    await page.route('**/api/threads/by-url?**', (route) => {
        const params = new URL(route.request().url()).searchParams;
        expect(params.get('url')).toBe(source);
        return route.fulfill({ json: {
            data: [{ title: '<img src=x onerror=alert(1)>', body: 'Text <script>bad()</script>', score: '<script>bad()</script>' }],
            meta: { total: 21, current_page: Number(params.get('page')), last_page: 2 },
        } });
    });
    await page.goto(`/discussions?${new URLSearchParams({ url: source })}`);
    await expect(page.getByRole('heading', { name: '<img src=x onerror=alert(1)>' })).toBeVisible();
    await expect(page.locator('.mf-thread img, .mf-thread script')).toHaveCount(0);
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expect(page.locator('[data-thread-page]')).toHaveText('Page 2 of 2');
    const request = page.waitForRequest((req) => req.url().includes('/api/threads/by-url') && new URL(req.url()).searchParams.get('sort') === 'new');
    await page.getByLabel('Sort').selectOption('new');
    expect(new URL((await request).url()).searchParams.get('page')).toBe('1');
    await expect(page.locator('[data-thread-page]')).toHaveText('Page 1 of 2');
});

test('late response bodies cannot replace a newer source selection', async ({ page, isMobile }) => {
    test.skip(isMobile, 'The side panel is a desktop interaction.');
    await page.goto('/search?q=community');
    await page.evaluate(() => {
        const original = window.fetch;
        window.fetch = (url, options) => {
            if (String(url).includes('/api/threads/by-url') && new URL(String(url)).searchParams.get('url').includes('/guide/1?')) {
                return Promise.resolve({ ok: true, json: () => new Promise((resolve) => {
                    window.releaseStaleBody = () => resolve({ data: [{ title: 'STALE SOURCE' }] });
                }) });
            }
            return original(url, options);
        };
    });
    await page.locator('[data-discuss-url]').nth(0).click();
    await page.waitForFunction(() => typeof window.releaseStaleBody === 'function');
    await page.keyboard.press('Escape');
    await page.locator('[data-discuss-url]').nth(1).click();
    await expect(page.getByRole('heading', { name: 'What makes this source useful?' })).toBeVisible();
    await page.evaluate(() => window.releaseStaleBody());
    await expect(page.locator('.mf-thread-list')).not.toContainText('STALE SOURCE');
    await expect(page.locator('[data-panel-source]')).toContainText('/guide/2?');
});

test('storage restrictions do not break theme or discussion loading', async ({ page }) => {
    await page.addInitScript(() => Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('Blocked', 'SecurityError'); } }));
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await page.goto('/discussions?url=https%3A%2F%2Fexample.test%2Fsource');
    await expect(page.getByRole('heading', { name: 'What makes this source useful?' })).toBeVisible();
    await page.locator('[data-theme-toggle]').click();
    expect(errors).toEqual([]);
});

test('beta thread submission is tied to its source and does not persist credentials', async ({ page }) => {
    const source = 'https://example.test/source?a=1&b=two';
    let posted;
    await page.route('**/api/threads', async (route) => {
        posted = { body: route.request().postDataJSON(), authorization: route.request().headers().authorization };
        await route.fulfill({ status: 201, json: { data: { id: 23 } } });
    });
    await page.goto(`/discussions?${new URLSearchParams({ url: source })}`);
    await page.locator('.mf-create-thread summary').click();
    await page.getByLabel('Forum API token').fill('browser-test-token');
    await page.getByLabel('Discussion title').fill('A question about this page');
    await page.getByLabel('Body optional').fill('Some useful context.');
    await page.getByRole('button', { name: 'Start thread', exact: true }).click();
    await expect(page.locator('[data-create-status]')).toHaveText('Thread created.');
    expect(posted).toEqual({
        authorization: 'Bearer browser-test-token',
        body: { title: 'A question about this page', body: 'Some useful context.', source_url: source },
    });
    expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain('browser-test-token');
    await expect(page.getByLabel('Discussion title')).toHaveValue('');
});

test('search and discussion fallback links work without JavaScript', async ({ browser, baseURL }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL });
    const page = await context.newPage();
    await page.goto('/');
    await page.getByRole('searchbox').fill('community');
    await page.getByRole('searchbox').press('Enter');
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await page.locator('[data-discuss-url]').first().click();
    await expect(page.getByText('Enable JavaScript to load community discussions.')).toBeVisible();
    await expect(page.locator('.mf-create-thread')).toBeHidden();
    await context.close();
});
