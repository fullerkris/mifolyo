import { randomUUID } from 'node:crypto';
import { test, expect } from '@playwright/test';
import { assertStaging, navigate } from './helpers.js';
import { accountSignIn, createAccount, createAccountThread, password, submitAccount } from './account-helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

const hint = (page, step) => page.locator(`[data-onboarding-step="${step}"]`);

async function state(page) {
    const response = await page.request.get('/onboarding');
    expect(response.status()).toBe(200);
    expect(response.headers()['cache-control']).toContain('no-store');
    return (await response.json()).data;
}

async function register(page, returnTo = '/') {
    const username = `onboard_${randomUUID().replaceAll('-', '').slice(0, 12)}`;
    const email = `${username}@staging.mifolyo.test`;
    await page.goto('/register?'+new URLSearchParams({ return_to: returnTo }));
    await submitAccount(page, 'Create account', async () => {
        await page.getByLabel('Display name', { exact: true }).fill('New reader');
        await page.getByLabel('Username', { exact: true }).fill(username);
        await page.getByLabel('Email', { exact: true }).fill(email);
        await page.getByLabel('Password', { exact: true }).fill(password);
        await page.getByLabel('Confirm password', { exact: true }).fill(password);
    });
    await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible();
    return { email, username };
}

async function screenshots(page, step, info) {
    for (const width of info.project.name === 'mobile' ? [320] : [768, 1440]) {
        await page.setViewportSize({ width, height: 1000 });
        for (const theme of ['light', 'dark']) {
            if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
            await expect(hint(page, step)).toBeVisible();
            expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
            await page.screenshot({ path: info.outputPath(`${step}-${width}-${theme}.png`), animations: 'disabled' });
        }
    }
}

test('welcome retries a failed dismissal, works without browser storage and stays dismissed after login', async ({ page }, info) => {
    await page.addInitScript(() => {
        for (const storage of ['localStorage', 'sessionStorage']) {
            Object.defineProperty(window, storage, { get() { throw new Error('Storage unavailable'); } });
        }
    });
    const account = await register(page);
    expect(await state(page)).toEqual({ welcome: true, discuss: true, vote: true });
    await screenshots(page, 'welcome', info);
    await page.route('**/onboarding', route => route.request().method() === 'POST'
        ? route.fulfill({ status: 503, json: { message: 'Deliberate save failure' } }) : route.continue());
    await page.getByRole('button', { name: 'Got it' }).click();
    await expect(page.locator('[data-onboarding-status]')).toContainText('try again');
    await expect(hint(page, 'welcome')).toBeVisible();
    expect((await state(page)).welcome).toBe(true);
    await page.unroute('**/onboarding');
    await page.getByRole('button', { name: 'Got it' }).focus();
    await page.keyboard.press('Enter');
    await expect(hint(page, 'welcome')).toBeHidden();
    await expect(page.getByRole('searchbox')).toBeFocused();
    await page.reload();
    await expect(hint(page, 'welcome')).toHaveCount(0);
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await page.goto('/sign-in');
    await accountSignIn(page, account.email);
    await expect(hint(page, 'welcome')).toHaveCount(0);
    expect(await state(page)).toEqual({ welcome: false, discuss: true, vote: true });
});

test('first search, Discuss and a confirmed vote complete guidance across browser sessions', async ({ page, browser, baseURL, isMobile }, info) => {
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const destination = '/search/exact?q=staging&page=2';
    const account = await register(page, destination);
    await expect(page).toHaveURL(baseURL+destination);
    await expect(hint(page, 'welcome')).toHaveCount(0);
    expect(await state(page)).toEqual({ welcome: false, discuss: true, vote: true });
    await expect(hint(page, 'discuss')).toHaveCount(1);
    await screenshots(page, 'discuss', info);
    const discuss = page.locator('[data-discuss-url]').first();
    await expect(discuss).toHaveAttribute('aria-describedby', 'onboarding-discuss');
    if (!isMobile) {
        await page.route('**/community/threads?**', route => route.fulfill({ status: 503, json: { message: 'Deliberate load failure' } }));
        await discuss.click();
        await expect(page.locator('[data-thread-status]')).toContainText('unavailable');
        expect((await state(page)).discuss).toBe(true);
        await page.unroute('**/community/threads?**');
        await page.getByRole('button', { name: 'Try again', exact: true }).click();
    } else {
        await discuss.click();
        expect(new URL(page.url()).pathname).toBe('/discussions');
        expect(new URL(page.url()).searchParams.get('from')).toBe(destination);
    }
    await expect.poll(async () => (await state(page)).discuss).toBe(false);
    if (!isMobile) {
        await page.getByRole('button', { name: 'Close discussions' }).click();
        await expect(hint(page, 'discuss')).toBeHidden();
        await expect(discuss).not.toHaveAttribute('aria-describedby', 'onboarding-discuss');
    }
    await navigate(page, () => page.goto('/search/exact?q=staging'));
    await expect(hint(page, 'discuss')).toHaveCount(0);
    await page.locator('[data-discuss-url]').first().click();
    await page.locator('[data-thread-list] .mf-thread-open').first().click();
    await expect(hint(page, 'vote')).toBeVisible();
    await screenshots(page, 'vote', info);
    const votes = page.locator('[data-vote-type="post"]');
    await page.route('**/threads/*/votes', route => route.fulfill({ status: 503, json: { message: 'Deliberate vote failure' } }));
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.locator('[data-vote-status]')).toContainText('couldn’t confirm');
    await expect(hint(page, 'vote')).toBeVisible();
    expect((await state(page)).vote).toBe(true);
    await page.unroute('**/threads/*/votes');
    await votes.getByRole('button', { name: 'Retry vote', exact: true }).click();
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'true');
    await expect(hint(page, 'vote')).toBeHidden();
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toBeFocused();
    await votes.getByRole('button', { name: /^Upvote/ }).click();
    await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'false');
    const threadURL = new URL(page.url()).pathname;
    await page.reload();
    await expect(hint(page, 'vote')).toHaveCount(0);
    expect(await state(page)).toEqual({ welcome: false, discuss: false, vote: false });
    const otherDevice = await browser.newContext({ baseURL });
    try {
        const otherPage = await otherDevice.newPage();
        await otherPage.goto('/sign-in?'+new URLSearchParams({ return_to: threadURL }));
        await accountSignIn(otherPage, account.email);
        await expect(otherPage.locator('[data-onboarding-step]')).toHaveCount(0);
        expect(await state(otherPage)).toEqual({ welcome: false, discuss: false, vote: false });
    } finally { await otherDevice.close(); }
    expect(errors).toEqual([]);
});

test('ordinary registration, dismissal and comment vote forms preserve the reading journey', async ({ browser, baseURL, isMobile, request }) => {
    const author = await createAccount(request);
    const { thread, comment } = await createAccountThread(request, author);
    const from = '/images?q=staging&page=2';
    const destination = `/threads/${thread}?${new URLSearchParams({ from, reply_to: String(comment) })}#comment-${comment}`;
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false, viewport: { width: isMobile ? 320 : 1440, height: 1000 } });
    try {
        const page = await context.newPage();
        await register(page, destination);
        await expect(page).toHaveURL(baseURL+destination);
        await expect(hint(page, 'welcome')).toBeVisible();
        await expect(hint(page, 'vote')).toBeVisible();
        expect(await state(page)).toEqual({ welcome: true, discuss: false, vote: true });
        await page.getByRole('button', { name: 'Got it' }).click();
        await expect(hint(page, 'welcome')).toHaveCount(0);
        expect(new URL(page.url()).searchParams.get('from')).toBe(from);
        expect(new URL(page.url()).searchParams.get('reply_to')).toBe(String(comment));
        const votes = page.locator(`[data-vote-type="comment"][data-vote-id="${comment}"]`);
        await votes.getByRole('button', { name: /^Downvote/ }).click();
        await expect(hint(page, 'vote')).toHaveCount(0);
        expect(new URL(page.url()).hash).toBe(`#comment-${comment}`);
        expect(new URL(page.url()).searchParams.get('from')).toBe(from);
        await votes.getByRole('button', { name: /^Downvote/ }).click();
        await expect(votes.locator('[data-vote-score]')).toHaveText('0');
        expect(await state(page)).toEqual({ welcome: false, discuss: false, vote: false });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    } finally { await context.close(); }
});

test('stale tabs refresh progress and cannot complete another account’s welcome', async ({ page, context, request }) => {
    const first = await createAccount(request);
    const second = await createAccount(request);
    await page.goto('/sign-in');
    await accountSignIn(page, first.email);
    await expect(hint(page, 'welcome')).toBeVisible();
    const otherTab = await context.newPage();
    try {
        await otherTab.goto('/');
        await otherTab.getByRole('button', { name: 'Sign out', exact: true }).click();
        await otherTab.goto('/sign-in');
        await accountSignIn(otherTab, second.email);
        await expect(hint(otherTab, 'welcome')).toBeVisible();
        const csrf = await otherTab.locator('meta[name="csrf-token"]').getAttribute('content');
        const stale = await page.request.post('/onboarding', { data: { step: 'welcome', user_id: first.user.id }, headers: { Accept: 'application/json', 'X-CSRF-TOKEN': csrf } });
        expect(stale.status()).toBe(409);
        expect((await state(otherTab)).welcome).toBe(true);
        // Headless Chromium keeps both tabs visible/focused; deliver the browser
        // lifecycle events explicitly, then verify the real account-state request.
        await page.evaluate(() => window.dispatchEvent(new Event('focus')));
        await expect(hint(page, 'welcome')).toBeHidden();
        await page.reload();
        await expect(hint(page, 'welcome')).toBeVisible();
        await otherTab.getByRole('button', { name: 'Got it' }).click();
        await expect(hint(otherTab, 'welcome')).toBeHidden();
        await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
        await expect(hint(page, 'welcome')).toBeHidden();
    } finally { await otherTab.close(); }
});
