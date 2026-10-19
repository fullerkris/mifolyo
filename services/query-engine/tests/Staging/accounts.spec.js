import { test, expect } from '@playwright/test';
import { assertStaging } from './helpers.js';
import { accountSignIn, api, createAccount, createAccountThread, password, resetMailLink, submitAccount } from './account-helpers.js';

test.beforeEach(async ({ request }) => assertStaging(request));

for (const noScript of [false, true]) {
    test(`email reset and password-confirmed deletion preserve conversations${noScript ? ' without JavaScript' : ''}`, async ({ browser, request, baseURL }, info) => {
        const account = await createAccount(request);
        const other = await createAccount(request);
        const { thread, comment } = await createAccountThread(request, account);
        for (const [type, id] of [['post', thread], ['comment', comment]]) {
            expect((await api(request, 'post', '/votes', { votable_type: type, votable_id: id, value: 1 }, account.token)).ok()).toBe(true);
            expect((await api(request, 'post', '/votes', { votable_type: type, votable_id: id, value: -1 }, other.token)).ok()).toBe(true);
        }
        const returnTo = `/threads/${thread}?from=%2Fimages%3Fq%3Dstaging%26page%3D2&reply_to=${comment}#comment-${comment}`;
        const context = await browser.newContext({ baseURL, javaScriptEnabled: !noScript, viewport: { width: info.project.name === 'mobile' ? 320 : 1440, height: 1000 } });
        try {
            const page = await context.newPage();
            await page.goto('/sign-in?'+new URLSearchParams({ return_to: returnTo }));
            await page.getByRole('link', { name: 'Forgot your password?' }).click();
            await submitAccount(page, 'Send reset link', () => page.getByLabel('Email', { exact: true }).fill(account.email));
            await expect(page.getByRole('status').filter({ hasText: 'If an account matches' })).toBeVisible();
            const link = await resetMailLink(request, account.email);
            const secret = new URL(link).searchParams.get('token');
            expect(new URL(link).searchParams.get('return_to')).toBe(returnTo);
            const response = await page.goto(link);
            expect(response.headers()['referrer-policy']).toBe('no-referrer');
            await expect(page).toHaveURL(`${baseURL}/reset-password`);
            expect(await page.content()).not.toContain(secret);
            const replacement = 'replacement-local-password';
            await submitAccount(page, 'Reset password', async () => {
                await page.getByLabel('New password', { exact: true }).fill(replacement);
                await page.getByLabel('Confirm new password').fill(replacement);
            });
            await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible();
            expect(new URL(page.url()).searchParams.get('return_to')).toBe(returnTo);
            expect((await api(request, 'get', '/auth/me', undefined, account.token)).status()).toBe(401);
            expect((await api(request, 'post', '/auth/login', { email: account.email, password })).status()).toBe(422);
            await accountSignIn(page, account.email, replacement);
            await expect(page).toHaveURL(baseURL+returnTo);
            await expect(page.locator('.mf-thread-detail .mf-author-level')).toHaveText('Level 1');
            await expect(page.locator(`#comment-${comment} .mf-author-level`)).toHaveText('Level 1');
            await expect(page.locator('[data-vote-score]').first()).toHaveText('0');
            await expect(page.getByRole('link', { name: '← Back to results' })).toHaveAttribute('href', '/images?q=staging&page=2#mf-return');
            await page.getByRole('link', { name: 'Your account', exact: true }).click();
            await expect(page.getByRole('heading', { name: 'Account settings' })).toBeVisible();
            await expect(page.getByText(account.email, { exact: true })).toBeVisible();
            await expect(page.getByText('Level 1', { exact: true })).toBeVisible();
            if (!noScript) {
                for (const width of info.project.name === 'mobile' ? [320] : [768, 1440]) {
                    await page.setViewportSize({ width, height: 1000 });
                    for (const theme of ['light', 'dark']) {
                        if (await page.locator('html').getAttribute('data-theme') !== theme) await page.locator('[data-theme-toggle]').click();
                        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
                        await page.screenshot({ path: info.outputPath(`account-${width}-${theme}.png`), fullPage: true, animations: 'disabled' });
                    }
                }
            }
            await submitAccount(page, 'Delete my account', async () => {
                await page.getByLabel('Current password').fill('incorrect-password');
                await page.getByLabel('I understand and want to delete my account.').check();
            });
            await expect(page.getByRole('alert')).toContainText('Check your current password');
            await expect(page.getByLabel('Current password')).toHaveValue('');
            await submitAccount(page, 'Delete my account', async () => {
                await page.getByLabel('Current password').fill(replacement);
                await page.getByLabel('I understand and want to delete my account.').check();
            });
            await expect(page).toHaveURL(`${baseURL}/`);
            await expect(page.getByRole('status').filter({ hasText: 'Your account has been deleted' })).toBeVisible();
            await expect(page.getByRole('link', { name: 'Sign in', exact: true })).toBeVisible();
            const persisted = (await (await api(request, 'get', `/posts/${thread}`)).json()).data;
            expect(persisted).toMatchObject({ author: null, score: -1, upvote_count: 0, downvote_count: 1, comment_count: 1 });
            const comments = (await (await api(request, 'get', `/posts/${thread}/comments`)).json()).data;
            expect(comments[0]).toMatchObject({ id: comment, author: null, body: 'Keep this reply too.', score: -1, upvote_count: 0, downvote_count: 1 });
            await page.goto(`/threads/${thread}`);
            await expect(page.locator('.mf-thread-detail .mf-author-name')).toHaveText('Community member');
            await expect(page.locator(`#comment-${comment} .mf-author-name`)).toHaveText('Community member');
            await expect(page.locator('.mf-author-level')).toHaveCount(0);
            expect((await api(request, 'post', '/auth/login', { email: account.email, password: replacement })).status()).toBe(422);
            await page.goto(link);
            await submitAccount(page, 'Reset password', async () => {
                await page.getByLabel('New password', { exact: true }).fill(replacement);
                await page.getByLabel('Confirm new password').fill(replacement);
            });
            await expect(page.getByRole('alert')).toContainText('invalid or has expired');
            await expect(page.getByRole('link', { name: 'Request a new reset link' })).toBeVisible();
        } finally {
            await context.close();
        }
    });
}
