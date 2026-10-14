import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, sourceURL } from './helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

test('home to indexed results to persisted source discussions', async ({ page, request, isMobile }, testInfo) => {
    await page.goto('/');
    await page.getByRole('searchbox').fill('staging');
    await page.getByRole('searchbox').press('Enter');
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await expect(page.getByText('25 results · Page 1')).toBeVisible();
    const first = page.locator('.mf-result-card').first();
    await expect(first.getByRole('link', { name: 'View page' })).toHaveAttribute('href', sourceURL);
    await page.screenshot({ path: testInfo.outputPath('real-search.png') });

    const threadsResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/threads/by-url');
    await first.locator('[data-discuss-url]').click();
    const response = await threadsResponse;
    expect(response.status()).toBe(200);
    expect(new URL(response.url()).searchParams.get('url')).toBe(sourceURL);
    const payload = await response.json();
    expect(payload.meta.total).toBe(23);
    expect(payload.data[0].source_url).toBe(sourceURL);
    expect(payload.data[0].comment_count).toBe(1);
    expect(payload.data[0].title).toBe('Source discussion 01');
    expect(payload.data[0].score).toBe(1);
    const comments = await request.get(`${forumURL}/api/posts/${payload.data[0].id}/comments`);
    expect(comments.ok()).toBe(true);
    expect(await comments.text()).toContain('A persisted staging reply');
    await expect(page.locator('.mf-thread')).toHaveCount(20);
    await expect(page.locator('[data-thread-page]')).toHaveText('Page 1 of 2');
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await expect(page.locator('.mf-thread')).toHaveCount(3);
    await expect(page.locator('[data-thread-page]')).toHaveText('Page 2 of 2');
    await page.getByLabel('Sort').selectOption('new');
    await expect(page.locator('[data-thread-page]')).toHaveText('Page 1 of 2');
    await expect(page.locator('.mf-thread').first()).toContainText('Source discussion 23');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('real-discussions.png') });

    if (isMobile) {
        expect(new URL(page.url()).pathname).toBe('/discussions');
        expect(new URL(page.url()).searchParams.get('url')).toBe(sourceURL);
        await page.goBack();
    } else {
        await page.keyboard.press('Escape');
        await expect(first.locator('[data-discuss-url]')).toBeFocused();
    }
    await expect(page.getByRole('searchbox')).toHaveValue('staging');
    await page.locator('[data-theme-toggle]').click();
    const theme = await page.locator('html').getAttribute('data-theme');
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
});

test('real search pagination preserves encoded query and browser history', async ({ page }) => {
    const query = 'staging & café +#';
    await page.goto('/');
    await page.getByRole('searchbox').fill(query);
    await page.getByRole('searchbox').press('Enter');
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await page.getByRole('link', { name: 'Next →', exact: true }).click();
    await expect(page.locator('.mf-result-card')).toHaveCount(5);
    await expect(page.getByRole('link', { name: 'Next →', exact: true })).toHaveCount(0);
    expect(new URL(page.url()).searchParams.get('q')).toBe(query);
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await page.reload();
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    await page.goBack();
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    await expect(page.locator('[data-navigation-status]')).toBeHidden();
});

test('dictionary correction can be declined and empty results have no phantom pages', async ({ page }) => {
    await page.goto('/search?q=stagng');
    await expect(page.locator('.mf-spelling')).toContainText('Showing results for staging.');
    await page.locator('.mf-spelling').getByRole('link', { name: 'stagng', exact: true }).click();
    await expect(page).toHaveURL(/\/search\/exact\?q=stagng/);
    await expect(page.getByRole('heading', { name: 'No results found' })).toBeVisible();
    await expect(page.getByRole('searchbox')).toHaveValue('stagng');
    await expect(page.getByRole('navigation', { name: 'Search result pages' })).toHaveCount(0);
});

test('forum login and UI thread creation persist on the selected source', async ({ page, request }, testInfo) => {
    const source = `https://example.test/local-staging/${randomUUID()}?a=1&b=two`;
    const title = `Staging browser discussion — ${testInfo.project.name}`;
    const login = await request.post(`${forumURL}/api/auth/login`, {
        data: { email: 'reader@staging.mifolyo.test', password: 'local-staging-only-password' },
    });
    expect(login.ok()).toBe(true);
    const { data: { token } } = await login.json();
    try {
        await page.goto(`/discussions?${new URLSearchParams({ url: source, title: 'Local staging write check' })}`);
        await expect(page.locator('[data-thread-status]')).toContainText('No discussions yet');
        await page.locator('.mf-create-thread summary').click();
        await page.getByLabel('Forum API token').fill(token);
        await page.getByLabel('Discussion title').fill(title);
        await page.getByLabel('Body optional').fill('Created through the real Query Engine proxy and stored by the forum in PostgreSQL.');
        const created = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/threads' && response.request().method() === 'POST');
        await page.getByRole('button', { name: 'Start thread', exact: true }).click();
        const response = await created;
        expect(response.status()).toBe(201);
        expect((await response.json()).data.source_url).toBe(source);
        await expect(page.locator('[data-create-status]')).toHaveText('Thread created.');
        await page.reload();
        await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
        const unrelated = await request.get(`/api/threads/by-url?${new URLSearchParams({ url: sourceURL })}`);
        expect((await unrelated.json()).meta.total).toBe(23);
    } finally {
        await request.post(`${forumURL}/api/auth/logout`, { headers: { Authorization: `Bearer ${token}` } });
    }
});
