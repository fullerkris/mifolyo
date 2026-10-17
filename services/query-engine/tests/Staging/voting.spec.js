import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, signIn } from './helpers.js';
import { createVotingConversation, voteGroup, votingOrigin } from './voting-helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

test('real thread and nested comment votes persist, repeat safely and keep independent totals', async ({ page, request }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const { thread, root, child } = await createVotingConversation(page);
    const votes = voteGroup(page, 'post', thread);
    const up = votes.getByRole('button', { name: /^Upvote/ });
    await up.focus();
    await page.keyboard.press('Enter');
    await expect(up).toHaveAttribute('aria-pressed', 'true');
    await expect(votes.locator('[data-vote-score]')).toHaveText('1');
    const csrf = await page.locator('meta[name="csrf-token"]').getAttribute('content');
    const duplicate = { _token: csrf, votable_type: 'post', votable_id: thread, value: 1, from: votingOrigin };
    const repeated = await Promise.all([1, 2].map(() => page.request.post(`/threads/${thread}/votes`, { form: duplicate, headers: { Accept: 'application/json' } })));
    for (const response of repeated) {
        expect(response.status()).toBe(200);
        expect((await response.json()).data.score).toBe(1);
    }
    const persisted = await request.get(`${forumURL}/api/posts/${thread}`);
    expect((await persisted.json()).data).toMatchObject({ score: 1, upvote_count: 1, downvote_count: 0, viewer_vote: 0 });
    await votes.getByRole('button', { name: /^Downvote/ }).click();
    await expect(votes.locator('[data-vote-score]')).toHaveText('-1');
    await votes.getByRole('button', { name: /^Downvote/ }).click();
    await expect(votes.locator('[data-vote-score]')).toHaveText('0');
    const nested = voteGroup(page, 'comment', child);
    await nested.getByRole('button', { name: /^Downvote/ }).click();
    await expect(nested.locator('[data-vote-score]')).toHaveText('-1');
    await page.reload();
    await expect(nested.getByRole('button', { name: /^Downvote/ })).toHaveAttribute('aria-pressed', 'true');
    await expect(voteGroup(page, 'comment', root).locator('[data-vote-score]')).toHaveText('0');
    const tree = (await (await request.get(`${forumURL}/api/posts/${thread}/comments`)).json()).data;
    expect(tree[0]).toMatchObject({ score: 0, upvote_count: 0, downvote_count: 0 });
    expect(tree[0].children[0]).toMatchObject({ score: -1, upvote_count: 0, downvote_count: 1, viewer_vote: 0 });
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.screenshot({ path: testInfo.outputPath(`real-votes-${theme}.png`), fullPage: true });
    }
    const rejected = await page.request.post(`/threads/${thread}/votes`, { data: { votable_type: 'post', votable_id: thread, value: 1 }, headers: { Accept: 'application/json' } });
    expect(rejected.status()).toBe(419);
    await nested.getByRole('button', { name: /^Downvote/ }).click();
    await expect(nested.locator('[data-vote-score]')).toHaveText('0');
    await page.getByRole('link', { name: 'Back to results' }).click();
    expect(new URL(page.url()).pathname).toBe('/images');
    expect(new URL(page.url()).searchParams.get('page')).toBe('2');
    expect(errors).toEqual([]);
    await page.getByRole('button', { name: 'Sign out' }).click();
});

test('token rotation returns to the exact comment and never replays a vote after sign-in', async ({ page, request }) => {
    const { thread, child } = await createVotingConversation(page);
    const votes = voteGroup(page, 'comment', child);
    await page.goto(`/threads/${thread}?${new URLSearchParams({ from: votingOrigin, reply_to: String(child) })}`);
    const rotated = await request.post(`${forumURL}/api/auth/login`, { data: { email: 'reader@staging.mifolyo.test', password: 'local-staging-only-password' } });
    expect(rotated.ok()).toBe(true);
    const submitted = page.waitForResponse(response => new URL(response.url()).pathname === `/threads/${thread}/votes`);
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    expect((await submitted).status()).toBe(401);
    await expect(votes.locator('[data-vote-status]')).toContainText('sign in again');
    await votes.getByRole('link', { name: 'Sign in to vote' }).click();
    await signIn(page);
    expect(new URL(page.url()).hash).toBe(`#comment-${child}`);
    expect(new URL(page.url()).searchParams.get('reply_to')).toBe(String(child));
    expect(new URL(page.url()).searchParams.get('from')).toBe(votingOrigin);
    await expect(votes.locator('[data-vote-score]')).toHaveText('0');
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'false');
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.locator('[data-vote-score]')).toHaveText('1');
    await page.getByRole('button', { name: 'Sign out' }).click();
});

test('real voting forms set, change and remove selections without JavaScript', async ({ browser, baseURL, isMobile, request }) => {
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    try {
        const page = await context.newPage();
        const { thread, child } = await createVotingConversation(page, true);
        for (const [type, id, anchor] of [['post', thread, '#thread-votes'], ['comment', child, `#comment-${child}`]]) {
            const votes = voteGroup(page, type, id);
            for (const [direction, score] of [['Upvote', '1'], ['Downvote', '-1'], ['Downvote', '0']]) {
                await votes.getByRole('button', { name: new RegExp(`^${direction}`) }).click();
                expect(new URL(page.url()).hash).toBe(anchor);
                expect(new URL(page.url()).searchParams.get('from')).toBe(votingOrigin);
                await expect(votes.locator('[data-vote-score]')).toHaveText(score);
            }
        }
        const persisted = await request.get(`${forumURL}/api/posts/${thread}`);
        expect((await persisted.json()).data).toMatchObject({ score: 0, upvote_count: 0, downvote_count: 0 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.getByRole('button', { name: 'Sign out' }).click();
    } finally { await context.close(); }
});
