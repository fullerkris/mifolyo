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
    const request = page.waitForRequest((request) => request.url().includes('/community/threads'));
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
    await page.route('**/community/threads?**', async (route) => {
        await new Promise((resolve) => { release = resolve; });
        await route.fulfill({ status: 503, json: { message: 'Unavailable' } });
    });
    await page.goto('/discussions?url=https%3A%2F%2Fexample.test%2Fsource');
    await expect(page.locator('[data-thread-status]')).toHaveText('Loading community threads…');
    await expect.poll(() => typeof release).toBe('function');
    release();
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
    await page.unroute('**/community/threads?**');
    await page.route('**/community/threads?**', (route) => route.fulfill({ json: { data: [], meta: { total: 0 } } }));
    await page.getByRole('button', { name: 'Try again' }).click();
    await expect(page.locator('[data-thread-status]')).toContainText('No discussions yet');
    await page.unroute('**/community/threads?**');
    await page.route('**/community/threads?**', (route) => route.fulfill({ json: { data: 'not-a-list' } }));
    await page.getByLabel('Sort').selectOption('new');
    await expect(page.locator('[data-thread-status]')).toContainText('unexpected response');
    await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
});

test('discussion page and sort requests keep the exact source and escape content', async ({ page }) => {
    const source = 'https://example.test/source?a=1&b=two+words';
    await page.route('**/community/threads?**', (route) => {
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
    const request = page.waitForRequest((req) => req.url().includes('/community/threads') && new URL(req.url()).searchParams.get('sort') === 'new');
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
            if (String(url).includes('/community/threads') && new URL(String(url)).searchParams.get('url').includes('/guide/1?')) {
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

test('guest sign-in preserves the selected source through registration links', async ({ page }) => {
    const source = 'https://example.test/source?a=1&b=two';
    await page.goto(`/discussions?${new URLSearchParams({ url: source })}`);
    await expect(page.getByLabel('Forum API token')).toHaveCount(0);
    await page.getByRole('link', { name: 'Sign in to start a thread' }).click();
    const returnTo = new URL(page.url()).searchParams.get('return_to');
    expect(new URL(returnTo, page.url()).searchParams.get('url')).toBe(source);
    await page.getByRole('link', { name: 'Create an account', exact: true }).click();
    expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
    await expect(page.getByLabel('Confirm password')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('conversation links open the full thread and accessible nested replies', async ({ page }, testInfo) => {
    await page.goto('/discussions?url=https%3A%2F%2Fexample.test%2Fsource');
    await page.getByRole('link', { name: 'Read conversation →' }).first().click();
    await expect(page).toHaveURL(/\/threads\/1$/);
    await expect(page.getByRole('heading', { name: 'What makes this source useful?' })).toBeVisible();
    await expect(page.locator('#comment-5')).toContainText('A nested perspective');
    await page.getByRole('link', { name: 'Reply to reader', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Reply to reader' })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath('thread-replies.png'), fullPage: true });
    await page.getByRole('link', { name: 'Sign in to reply', exact: true }).click();
    expect(new URL(page.url()).searchParams.get('return_to')).toBe('/threads/1?reply_to=4');
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
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.getByRole('searchbox')).toHaveValue('community');
    await context.close();
});

test('thread navigation restores the exact search, scroll, source and discussion panel', async ({ page, isMobile }) => {
    const query = 'C++ & café #1';
    await page.route('**/community/threads?**', route => {
        const params = new URL(route.request().url()).searchParams;
        return route.fulfill({ json: { data: [{ id: 1, title: 'A source conversation' }], meta: { total: 21, current_page: Number(params.get('page')), last_page: 2 } } });
    });
    await page.goto(`/search/exact?${new URLSearchParams({ q: query, page: '2' })}`);
    const selected = page.locator('[data-discuss-url]').nth(7);
    await expect(selected.locator('[data-discussion-count]')).toHaveText('0');
    await selected.scrollIntoViewIfNeeded();
    const source = await selected.getAttribute('data-discuss-url');
    const scroll = await page.evaluate(() => scrollY);
    expect(scroll).toBeGreaterThan(500);
    await selected.click();
    if (!isMobile) {
        await page.getByRole('combobox', { name: 'Sort', exact: true }).selectOption('new');
        await page.getByRole('button', { name: 'Next', exact: true }).click();
        await expect(page.locator('[data-thread-page]')).toHaveText('Page 2 of 2');
    }
    await page.getByRole('link', { name: 'Read conversation →' }).click();
    const from = new URL(page.url()).searchParams.get('from');
    expect(new URL(from, page.url()).pathname).toBe('/search/exact');
    expect(new URL(from, page.url()).searchParams.get('q')).toBe(query);
    expect(new URL(from, page.url()).searchParams.get('page')).toBe('2');

    // Browser Back restores the desktop panel, or the mobile list that led to the thread.
    await page.goBack();
    if (!isMobile) {
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.getByRole('combobox', { name: 'Sort', exact: true })).toHaveValue('new');
        await expect(page.locator('[data-thread-page]')).toHaveText('Page 2 of 2');
    }
    await page.goForward();
    await page.reload();
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    if (!isMobile) {
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.locator('[data-panel-source]')).toHaveAttribute('href', source);
        await expect(page.locator('[data-thread-page]')).toHaveText('Page 2 of 2');
        await page.getByRole('button', { name: 'Close discussions' }).click();
    }
    await expect(selected).toBeFocused();
    await expect.poll(async () => Math.abs(await page.evaluate(() => scrollY) - scroll)).toBeLessThan(5);
    await page.reload();
    await expect(page.getByRole('dialog')).not.toBeVisible();
});

test('return context survives sign-in links and blocked storage with a safe direct-entry fallback', async ({ page }) => {
    await page.addInitScript(() => {
        for (const name of ['localStorage', 'sessionStorage']) Object.defineProperty(window, name, { get() { throw new DOMException('Blocked', 'SecurityError'); } });
    });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const from = '/search/exact?q=source%20quality&page=2';
    await page.goto(`/threads/1?${new URLSearchParams({ from })}`);
    await page.getByRole('link', { name: 'Sign in to reply', exact: true }).click();
    await page.getByRole('link', { name: 'Create an account', exact: true }).click();
    const returnTo = new URL(page.url()).searchParams.get('return_to');
    expect(new URL(returnTo, page.url()).searchParams.get('from')).toBe(from);
    await page.goto(returnTo);
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.getByRole('searchbox')).toHaveValue('source quality');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await page.goto('/threads/1');
    await expect(page.locator('[data-back-to-results]')).toHaveCount(0);
    await expect(page.getByRole('link', { name: 'All discussions for this source' })).toBeVisible();
    expect(errors).toEqual([]);
});

test('conversation counts use one batch and distinguish loading, zero, unavailable and retry', async ({ page }) => {
    let release;
    const requests = [];
    await page.route('**/community/thread-counts', async route => {
        const request = route.request();
        requests.push(request);
        if (requests.length === 1) await new Promise(resolve => { release = resolve; });
        const { urls } = request.postDataJSON();
        await route.fulfill({ json: { data: urls.map((url, index) => ({ url, count: index === 0 ? 23 : index === 2 && requests.length === 1 ? null : 0 })) } });
    });
    await page.goto('/search?q=community');
    await expect(page.locator('[data-discussion-count]').first()).toHaveText('…');
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await expect.poll(() => typeof release).toBe('function');
    release();
    const badges = page.locator('[data-discussion-count]');
    await expect(badges.nth(0)).toHaveText('23');
    await expect(badges.nth(0)).toHaveAttribute('aria-label', '23 conversations');
    await expect(badges.nth(1)).toHaveText('0');
    await expect(badges.nth(2)).toHaveText('—');
    expect(requests).toHaveLength(1);
    expect(requests[0].postDataJSON().urls).toHaveLength(20);
    expect(requests[0].headers()['x-csrf-token']).toBeTruthy();
    await page.getByRole('button', { name: 'Retry counts' }).click();
    await expect(badges.nth(2)).toHaveText('0');
    await expect(page.locator('[data-counts-status]')).toBeHidden();
    expect(requests).toHaveLength(2);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('late batch counts cannot replace newer conversation totals', async ({ page, isMobile }) => {
    test.skip(isMobile, 'The panel remains on the results page only on desktop.');
    let release;
    await page.route('**/community/thread-counts', async route => {
        await new Promise(resolve => { release = resolve; });
        await route.fulfill({ json: { data: route.request().postDataJSON().urls.map(url => ({ url, count: 0 })) } });
    });
    await page.goto('/search?q=community');
    await page.locator('[data-discuss-url]').first().click();
    await expect(page.locator('[data-thread-status]')).toContainText('2 conversations');
    await expect.poll(() => typeof release).toBe('function');
    release();
    await page.getByRole('button', { name: 'Close discussions' }).click();
    await expect(page.locator('[data-discussion-count]').nth(1)).toHaveText('0');
    await expect(page.locator('[data-discussion-count]').first()).toHaveText('2');
});
