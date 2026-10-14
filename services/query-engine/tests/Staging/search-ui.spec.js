import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, sourceURL, signIn } from './helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

test('home to indexed results to persisted source discussions', async ({ page, request, isMobile }, testInfo) => {
    await page.goto('/');
    await page.getByRole('searchbox').fill('staging');
    await page.getByRole('searchbox').press('Enter');
    await expect(page.locator('.mf-result-card')).toHaveCount(20);
    await expect(page.getByText('25 results · Page 1')).toBeVisible();
    const first = page.locator('.mf-result-card').first();
    await expect(first.getByRole('link', { name: 'View page' })).toHaveAttribute('href', sourceURL);
    await expect(first.locator('[data-discussion-count]')).toHaveText('23');
    await expect(page.locator('[data-discussion-count]').nth(1)).toHaveText('0');
    await page.screenshot({ path: testInfo.outputPath('real-search.png') });

    const threadsResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/community/threads');
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

test('sign-in, thread creation, nested replies and expired-session drafts persist', async ({ page, request, isMobile }, testInfo) => {
    const source = `https://example.test/local-staging/${randomUUID()}?a=1&b=two`;
    const from = '/search/exact?q=staging&page=2';
    const title = `Staging browser discussion — ${testInfo.project.name}`;
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));
    try {
        await page.goto(`/discussions?${new URLSearchParams({ url: source, title: 'Local staging write check', from })}`);
        await expect(page.locator('[data-thread-status]')).toContainText('No discussions yet');
        await page.getByRole('link', { name: 'Sign in to start a thread' }).click();
        if (!isMobile) {
            await page.getByLabel('Email', { exact: true }).fill('reader@staging.mifolyo.test');
            await page.getByLabel('Password', { exact: true }).fill('incorrect-password');
            await page.getByRole('button', { name: 'Sign in', exact: true }).click();
            await expect(page.getByRole('alert')).toContainText('Invalid credentials');
            await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
        }
        await signIn(page);
        expect(new URL(page.url()).searchParams.get('from')).toBe(from);
        expect(new URL(page.url()).searchParams.get('url')).toBe(source);
        await page.locator('.mf-create-thread summary').click();
        await page.getByLabel('Discussion title').fill(title);
        await page.getByLabel('Body optional').fill('Created through the real Query Engine proxy and stored by the forum in PostgreSQL.');
        const created = page.waitForResponse((response) => new URL(response.url()).pathname === '/community/threads' && response.request().method() === 'POST');
        await page.getByRole('button', { name: 'Start thread', exact: true }).click();
        const response = await created;
        expect(response.status()).toBe(201);
        const thread = (await response.json()).data;
        expect(thread.source_url).toBe(source);
        expect(response.request().headers().authorization).toBeUndefined();
        expect(response.request().headers()['x-csrf-token']).toBeTruthy();
        await page.getByRole('link', { name: 'Open your conversation →' }).click();
        await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
        await expect(page.getByText('No replies yet. Add the first perspective.')).toBeVisible();
        const csrfRejected = await page.request.post(`/threads/${thread.id}/comments`, { data: { body: 'CSRF must reject this' }, headers: { Accept: 'application/json' } });
        expect(csrfRejected.status()).toBe(419);
        await page.getByLabel('Your reply').fill('A root perspective from the browser.');
        await page.getByRole('button', { name: 'Post reply', exact: true }).click();
        await expect(page.locator('.mf-comment-body')).toHaveText('A root perspective from the browser.');
        await page.locator('.mf-comment').first().getByRole('link', { name: /^Reply/ }).click();
        await page.getByLabel('Your reply').fill('A nested reply from the browser.');
        await page.getByRole('button', { name: 'Post reply', exact: true }).click();
        await expect(page.locator('.mf-comment-children')).toContainText('A nested reply from the browser.');

        // A separate real login rotates the forum token, invalidating this browser's session.
        await page.locator('.mf-comment > article').first().getByRole('link', { name: /^Reply/ }).click();
        const rotation = await request.post(`${forumURL}/api/auth/login`, {
            data: { email: 'reader@staging.mifolyo.test', password: 'local-staging-only-password' },
        });
        expect(rotation.ok()).toBe(true);
        await page.getByLabel('Your reply').fill('Draft kept after the token expired.');
        await page.getByRole('button', { name: 'Post reply', exact: true }).click();
        await expect(page).toHaveURL(/\/sign-in\?/);
        await signIn(page);
        expect(new URL(page.url()).searchParams.get('from')).toBe(from);
        await expect(page.getByRole('heading', { name: /^Reply to / })).toBeVisible();
        await expect(page.getByLabel('Your reply')).toHaveValue('Draft kept after the token expired.');
        await page.getByRole('button', { name: 'Post reply', exact: true }).click();
        await expect(page.locator('.mf-comment-children')).toContainText('Draft kept after the token expired.');
        await page.reload();
        await expect(page.locator('.mf-thread-meta').first()).toContainText('3 replies');
        const comments = await request.get(`${forumURL}/api/posts/${thread.id}/comments`);
        const tree = (await comments.json()).data;
        expect(tree).toHaveLength(1);
        expect(tree[0].children.map((child) => child.body)).toEqual(['A nested reply from the browser.', 'Draft kept after the token expired.']);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.screenshot({ path: testInfo.outputPath('real-thread-replies.png'), fullPage: true });
        expect(await page.evaluate(() => Object.keys(localStorage))).not.toContain('forum.token');
        expect(errors).toEqual([]);
        const unrelated = await request.get(`/api/threads/by-url?${new URLSearchParams({ url: sourceURL })}`);
        expect((await unrelated.json()).meta.total).toBe(23);
    } finally {
        await page.goto('/');
        if (await page.getByRole('button', { name: 'Sign out' }).count()) await page.getByRole('button', { name: 'Sign out' }).click();
    }
});

test('registration returns to the selected conversation and sign-out ends access to posting', async ({ page, request }, testInfo) => {
    const threads = await request.get(`/api/threads/by-url?${new URLSearchParams({ url: sourceURL })}`);
    const thread = (await threads.json()).data[0];
    const username = `staging_${randomUUID().replaceAll('-', '').slice(0, 16)}`;
    await page.goto(`/threads/${thread.id}`);
    await page.getByRole('link', { name: 'create an account', exact: true }).click();
    await page.getByLabel('Display name').fill('Staging browser reader');
    await page.getByLabel('Username', { exact: true }).fill(username);
    await page.getByLabel('Email', { exact: true }).fill(`${username}@staging.mifolyo.test`);
    await page.getByLabel('Password', { exact: true }).fill('local-staging-only-password');
    await page.getByLabel('Confirm password').fill('local-staging-only-password');
    await page.screenshot({ path: testInfo.outputPath('registration.png'), fullPage: true });
    await page.getByRole('button', { name: 'Create account', exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/threads/${thread.id}$`));
    await expect(page.locator('.mf-account-name')).toHaveText(username);
    const cookie = (await page.context().cookies()).find((cookie) => cookie.name === 'mifolyo_search_staging_session');
    expect(cookie.httpOnly).toBe(true);
    expect(cookie.sameSite).toBe('Lax');
    await page.getByRole('button', { name: 'Sign out' }).click();
    await page.goto(`/threads/${thread.id}`);
    await expect(page.getByRole('link', { name: 'Sign in to reply' })).toBeVisible();
    await expect(page.getByLabel('Your reply')).toHaveCount(0);
});

test('exact results context and live counts survive sign-in, thread creation and replies', async ({ page, isMobile }, testInfo) => {
    const query = 'staging & café +#';
    await page.goto(`/search/exact?${new URLSearchParams({ q: query, page: '2' })}`);
    const selected = page.locator('[data-discuss-url]').nth(3);
    await expect(selected.locator('[data-discussion-count]')).toHaveText(/^\d+$/);
    const before = Number(await selected.locator('[data-discussion-count]').textContent());
    const source = await selected.getAttribute('data-discuss-url');
    await selected.scrollIntoViewIfNeeded();
    const scroll = await page.evaluate(() => scrollY);
    await selected.click();
    await page.getByRole('link', { name: 'Sign in to start a thread' }).click();
    await signIn(page);
    expect(new URL(page.url()).searchParams.get('url')).toBe(source);
    const from = new URL(page.url()).searchParams.get('from');
    expect(new URL(from, page.url()).pathname).toBe('/search/exact');
    expect(new URL(from, page.url()).searchParams.get('q')).toBe(query);
    expect(new URL(from, page.url()).searchParams.get('page')).toBe('2');
    await page.locator('.mf-create-thread summary').click();
    await page.getByLabel('Discussion title').fill(`Search return check ${randomUUID()}`);
    await page.getByRole('button', { name: 'Start thread', exact: true }).click();
    await page.getByRole('link', { name: 'Open your conversation →' }).click();
    await page.getByLabel('Your reply').fill('Returning to the same search after this reply.');
    await page.getByRole('button', { name: 'Post reply', exact: true }).click();
    await expect(page.locator('.mf-comment-body')).toHaveText('Returning to the same search after this reply.');
    expect(new URL(page.url()).searchParams.get('from')).toBe(from);
    await page.getByRole('link', { name: 'Back to results' }).click();
    await expect(page.getByRole('searchbox')).toHaveValue(query);
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    if (!isMobile) {
        await expect(page.getByRole('dialog')).toBeVisible();
        await expect(page.locator('[data-panel-source]')).toHaveAttribute('href', source);
        await page.getByRole('button', { name: 'Close discussions' }).click();
    }
    await expect(selected).toBeFocused();
    await expect(selected.locator('[data-discussion-count]')).toHaveText(String(before + 1));
    await expect.poll(async () => Math.abs(await page.evaluate(() => scrollY) - scroll)).toBeLessThan(5);
    await page.screenshot({ path: testInfo.outputPath('restored-results.png') });
    await page.getByRole('button', { name: 'Sign out' }).click();
});

test('header sign-in returns to the exact search page', async ({ page }) => {
    await page.goto('/search/exact?q=staging&page=2');
    await page.getByRole('link', { name: 'Sign in', exact: true }).click();
    expect(new URL(page.url()).searchParams.get('return_to')).toBe('/search/exact?q=staging&page=2#mf-return');
    await signIn(page);
    await expect(page.getByRole('searchbox')).toHaveValue('staging');
    expect(new URL(page.url()).pathname).toBe('/search/exact');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    await page.getByRole('button', { name: 'Sign out' }).click();
});

test('account and reply forms work without JavaScript', async ({ browser, request }) => {
    const context = await browser.newContext({ javaScriptEnabled: false, baseURL: 'http://127.0.0.1:18081' });
    const page = await context.newPage();
    try {
        const source = `https://example.test/no-js/${randomUUID()}`;
        const from = '/search/exact?q=staging&page=2';
        await page.goto(`/discussions?${new URLSearchParams({ url: source, from })}`);
        await page.getByRole('link', { name: 'Sign in to start a thread' }).click();
        await signIn(page);
        await page.locator('.mf-create-thread summary').click();
        await page.getByLabel('Discussion title').fill('A conversation without JavaScript');
        await page.getByRole('button', { name: 'Start thread', exact: true }).click();
        await expect(page).toHaveURL(/\/threads\/\d+\?/);
        expect(new URL(page.url()).searchParams.get('from')).toBe(from);
        await page.getByLabel('Your reply').fill('A persisted no-JavaScript reply.');
        await page.getByRole('button', { name: 'Post reply', exact: true }).click();
        await expect(page.locator('.mf-comment-body')).toHaveText('A persisted no-JavaScript reply.');
        const threadId = new URL(page.url()).pathname.split('/').pop();
        const comments = await request.get(`${forumURL}/api/posts/${threadId}/comments`);
        expect((await comments.json()).data[0].body).toBe('A persisted no-JavaScript reply.');
        await page.getByRole('link', { name: 'Back to results' }).click();
        await expect(page.getByRole('searchbox')).toHaveValue('staging');
        expect(new URL(page.url()).pathname).toBe('/search/exact');
        expect(new URL(page.url()).searchParams.get('page')).toBe('2');
        await page.getByRole('button', { name: 'Sign out' }).click();
    } finally {
        await context.close();
    }
});
