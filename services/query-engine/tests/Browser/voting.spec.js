import { test, expect } from '@playwright/test';

const threadVotes = page => page.locator('#thread-votes');
const commentVotes = (page, id) => page.locator(`[data-vote-type="comment"][data-vote-id="${id}"]`);
const from = '/images?q=C%2B%2B%20%26%20caf%C3%A9&page=2';
const reading = `/threads/1?${new URLSearchParams({ from, reply_to: '4' })}`;
async function signIn(page) {
    await page.getByLabel('Email', { exact: true }).fill('reader@example.test');
    await page.getByLabel('Password', { exact: true }).fill('fixture-password');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
}
async function openSignedIn(page) {
    await page.goto(reading);
    await threadVotes(page).getByRole('link', { name: 'Sign in to vote' }).click();
    await signIn(page);
}

test.beforeEach(async ({ page }) => {
    const response = await page.goto('/');
    expect(response.headers()['x-mifolyo-browser-fixture']).toBe('1');
});

test('guest voting sign-in preserves the exact comment, reply target and search origin', async ({ page }) => {
    await page.goto(reading);
    const votes = commentVotes(page, 5);
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toBeDisabled();
    await votes.getByRole('link', { name: 'Sign in to vote' }).click();
    const back = new URL(new URL(page.url()).searchParams.get('return_to'), page.url());
    expect(back.hash).toBe('#comment-5');
    expect(back.searchParams.get('from')).toBe(from);
    expect(back.searchParams.get('reply_to')).toBe('4');
    await signIn(page);
    expect(new URL(page.url()).hash).toBe('#comment-5');
    expect(new URL(page.url()).searchParams.get('from')).toBe(from);
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'false');
    await expect(votes.locator('[data-vote-score]')).toHaveText('2');
});

test('post and nested comment votes change, clear and persist with keyboard and theme support', async ({ page }, testInfo) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await openSignedIn(page);
    const votes = threadVotes(page);
    await page.getByLabel('Your reply').fill('Keep this unfinished reply while voting.');
    const up = votes.getByRole('button', { name: /^Upvote/ });
    await up.focus();
    await page.keyboard.press('Enter');
    await expect(up).toHaveAttribute('aria-pressed', 'true');
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
    await expect(up).toBeFocused();
    await page.keyboard.press('Space');
    await expect(up).toHaveAttribute('aria-pressed', 'false');
    await expect(votes.locator('[data-vote-score]')).toHaveText('4');
    await page.keyboard.press('Enter');
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
    await votes.getByRole('button', { name: /^Downvote/ }).click();
    await expect(up).toHaveAttribute('aria-pressed', 'false');
    await expect(votes.locator('[data-vote-score]')).toHaveText('3');
    await votes.getByRole('button', { name: /^Downvote/ }).click();
    await expect(votes.locator('[data-vote-score]')).toHaveText('4');
    await expect(votes.locator('[data-vote-status]')).toContainText('Vote removed.');
    await commentVotes(page, 5).getByRole('button', { name: /^Upvote/ }).click();
    await expect(commentVotes(page, 5).locator('[data-vote-score]')).toHaveText('3');
    await expect(commentVotes(page, 4).locator('[data-vote-score]')).toHaveText('2');
    await expect(commentVotes(page, 6).getByRole('button', { name: /^Upvote/ })).toBeDisabled();
    await expect(commentVotes(page, 7).locator('button')).toHaveCount(0);
    await expect(page.getByLabel('Your reply')).toHaveValue('Keep this unfinished reply while voting.');
    await page.reload();
    await expect(commentVotes(page, 5).getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'true');
    for (const theme of ['light', 'dark']) {
        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.evaluate(() => window.scrollTo(0, 0));
        await page.screenshot({ path: testInfo.outputPath(`voting-${theme}.png`), fullPage: true, animations: 'disabled' });
    }
    expect(errors).toEqual([]);
    expect(await page.content()).not.toContain('browser-fixture-server-token');
});

test('saving prevents duplicate submits and waits for a confirmed server score', async ({ page }) => {
    await openSignedIn(page);
    let release;
    let calls = 0;
    await page.route('**/threads/1/votes', async route => {
        calls++;
        await new Promise(resolve => { release = resolve; });
        await route.continue();
    });
    const votes = threadVotes(page);
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.locator('[data-vote-status]')).toHaveText('Saving vote…');
    await expect(votes.getByRole('button', { name: /^Downvote/ })).toBeDisabled();
    await expect(votes.locator('[data-vote-score]')).toHaveText('4');
    await votes.getByRole('button', { name: /^Upvote/ }).dispatchEvent('click');
    expect(calls).toBe(1);
    release();
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
});

test('retrying a lost successful response sets the same vote exactly once', async ({ page }) => {
    await openSignedIn(page);
    let calls = 0;
    await page.route('**/threads/1/votes', async route => {
        calls++;
        const response = await route.fetch();
        expect(response.status()).toBe(200);
        if (calls === 1) await route.abort();
        else await route.fulfill({ response });
    });
    const votes = threadVotes(page);
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.locator('[data-vote-status]')).toContainText('couldn’t confirm');
    await expect(votes.locator('[data-vote-score]')).toHaveText('4');
    await votes.getByRole('button', { name: 'Retry vote' }).focus();
    await page.keyboard.press('Enter');
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toBeFocused();
    await page.reload();
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'true');
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
    expect(calls).toBe(2);
});

test('throttled votes respect Retry-After and reject malformed success', async ({ page }) => {
    await openSignedIn(page);
    let calls = 0;
    await page.route('**/threads/1/votes', route => {
        calls++;
        if (calls === 1) return route.fulfill({ status: 429, headers: { 'Retry-After': '1' }, json: { message: 'Rate limited' } });
        if (calls === 2) return route.fulfill({ json: { data: { type: 'post', id: 99, viewer_vote: 1, score: 999 } } });
        return route.continue();
    });
    const votes = threadVotes(page);
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.locator('[data-vote-status]')).toContainText('Too many votes');
    await expect(votes.getByRole('button', { name: 'Retry vote' })).toBeDisabled();
    await expect(votes.getByRole('button', { name: 'Retry vote' })).toBeEnabled();
    expect(calls).toBe(1);
    await votes.getByRole('button', { name: 'Retry vote' }).click();
    await expect(votes.locator('[data-vote-status]')).toContainText('couldn’t confirm');
    await expect(votes.locator('[data-vote-score]')).toHaveText('4');
    await votes.getByRole('button', { name: 'Retry vote' }).click();
    await expect(votes.locator('[data-vote-score]')).toHaveText('5');
});

test('closed, missing, forbidden and expired-session votes leave an actionable reading page', async ({ page }) => {
    await openSignedIn(page);
    for (const status of [403, 404, 419, 423, 401]) {
        await page.reload();
        await page.route('**/threads/1/votes', route => route.fulfill({ status, json: { message: '<script>untrusted()</script>' } }));
        const votes = commentVotes(page, 5);
        await votes.getByRole('button', { name: /^Upvote/ }).click();
        await expect(votes.getByRole('button', { name: /^Upvote/ })).toBeDisabled();
        if (status === 401) {
            await expect(threadVotes(page).getByRole('button', { name: /^Upvote/ })).toBeDisabled();
            const link = votes.getByRole('link', { name: 'Sign in to vote' });
            await expect(link).toBeVisible();
            const returnTo = new URL(await link.getAttribute('href')).searchParams.get('return_to');
            expect(new URL(returnTo, page.url()).hash).toBe('#comment-5');
        } else await expect(votes.getByRole('link', { name: 'Reload conversation' })).toBeVisible();
        await expect(votes.locator('[data-vote-score]')).toHaveText('2');
        await page.unroute('**/threads/1/votes');
    }
});

test('votes and sign-in return work without JavaScript', async ({ browser, baseURL, isMobile }) => {
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    try {
        const page = await context.newPage();
        await openSignedIn(page);
        const votes = commentVotes(page, 5);
        for (const [direction, score, selected] of [['Upvote', '3', 'true'], ['Downvote', '1', 'true'], ['Downvote', '2', 'false']]) {
            await votes.getByRole('button', { name: new RegExp(`^${direction}`) }).click();
            expect(new URL(page.url()).hash).toBe('#comment-5');
            expect(new URL(page.url()).searchParams.get('from')).toBe(from);
            expect(new URL(page.url()).searchParams.get('reply_to')).toBe('4');
            await expect(votes.locator('[data-vote-score]')).toHaveText(score);
            await expect(votes.getByRole('button', { name: new RegExp(`^${direction}`) })).toHaveAttribute('aria-pressed', selected);
        }
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    } finally { await context.close(); }
});
