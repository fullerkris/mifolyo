import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test, expect } from '@playwright/test';
import { assertStaging, forumURL, sourceURL, waitUntilReady } from './helpers.js';
import { createVotingConversation, voteGroup } from './voting-helpers.js';
import { accountSignIn, createAccount, resetMailLink, submitAccount } from './account-helpers.js';

const root = fileURLToPath(new URL('../../../../', import.meta.url));
const compose = (...args) => execFileSync('docker', [
    'compose', '--project-name', 'mifolyo-search-ui-staging',
    '--file', 'scripts/docker/search-ui-staging.compose.yml', ...args,
], { cwd: root, timeout: 30000, stdio: 'pipe' });

test.beforeEach(async ({ request }) => assertStaging(request));

test('reset mail delivery recovers after a real SMTP outage', async ({ page, request }, testInfo) => {
    const account = await createAccount(request);
    let stopped = false;
    try {
        await page.goto('/forgot-password?return_to=%2Fimages%3Fq%3Dstaging%26page%3D2');
        compose('stop', '--timeout', '5', 'mail');
        stopped = true;
        await submitAccount(page, 'Send reset link', () => page.getByLabel('Email', { exact: true }).fill(account.email));
        await expect(page.getByRole('alert')).toContainText('temporarily unavailable');
        await expect(page.getByLabel('Email', { exact: true })).toHaveValue(account.email);
        await page.screenshot({ path: testInfo.outputPath('reset-mail-unavailable.png'), fullPage: true });
        compose('start', 'mail');
        await waitUntilReady(request, 'http://127.0.0.1:18085/readyz');
        stopped = false;
        await submitAccount(page, 'Send reset link', () => page.getByLabel('Email', { exact: true }).fill(account.email));
        await expect(page.getByRole('status').filter({ hasText: 'If an account matches' })).toBeVisible();
        expect(new URL(await resetMailLink(request, account.email)).searchParams.get('return_to')).toBe('/images?q=staging&page=2');
    } finally {
        if (stopped) {
            compose('start', 'mail');
            await waitUntilReady(request, 'http://127.0.0.1:18085/readyz');
        }
    }
});

test('a vote survives a real forum outage through an explicit idempotent retry', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        const account = await createAccount(request);
        const { thread } = await createVotingConversation(page, false, page => accountSignIn(page, account.email));
        const votes = voteGroup(page, 'post', thread);
        const voteHint = page.locator('[data-onboarding-step="vote"]');
        await expect(voteHint).toBeVisible();
        compose('stop', '--timeout', '5', 'forum-engine');
        stopped = true;
        await page.getByRole('button', { name: 'Got it' }).click();
        await expect(page.locator('[data-onboarding-status]')).toContainText('try again', { timeout: 15000 });
        await expect(page.locator('[data-onboarding-step="welcome"]')).toBeVisible();
        await votes.getByRole('button', { name: /^Upvote/ }).click();
        await expect(votes.locator('[data-vote-status]')).toContainText('couldn’t confirm', { timeout: 15000 });
        await expect(votes.locator('[data-vote-score]')).toHaveText('0');
        await expect(voteHint).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('vote-unavailable.png'), fullPage: true });
        compose('start', 'forum-engine');
        await waitUntilReady(request, `${forumURL}/api/health/ready`);
        stopped = false;
        expect((await (await page.request.get('/onboarding')).json()).data).toMatchObject({ welcome: true, vote: true });
        await page.getByRole('button', { name: 'Got it' }).click();
        await expect(page.locator('[data-onboarding-step="welcome"]')).toBeHidden();
        await votes.getByRole('button', { name: 'Retry vote' }).click();
        await expect(votes.locator('[data-vote-score]')).toHaveText('1');
        await expect(voteHint).toBeHidden();
        await page.reload();
        await expect(votes.getByRole('button', { name: /^Upvote/ })).toHaveAttribute('aria-pressed', 'true');
        const persisted = await request.get(`${forumURL}/api/posts/${thread}`);
        expect((await persisted.json()).data).toMatchObject({ score: 1, upvote_count: 1, downvote_count: 0 });
        await page.getByRole('button', { name: 'Sign out' }).click();
    } finally {
        if (stopped) {
            compose('start', 'forum-engine');
            await waitUntilReady(request, `${forumURL}/api/health/ready`);
        }
    }
});

test('forum outage shows retry and recovers against the real service', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        compose('stop', '--timeout', '5', 'forum-engine');
        stopped = true;
        await page.goto('/search?q=staging');
        await expect(page.locator('.mf-result-card')).toHaveCount(20);
        await expect(page.locator('[data-discussion-count]').first()).toHaveText('—', { timeout: 15000 });
        await expect(page.locator('[data-counts-status]')).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('counts-unavailable.png') });
        await page.locator('[data-discuss-url]').first().click();
        await expect(page.locator('[data-thread-status]')).toContainText('unavailable', { timeout: 15000 });
        await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
        await page.screenshot({ path: testInfo.outputPath('forum-unavailable.png') });
        compose('start', 'forum-engine');
        await waitUntilReady(request, `${forumURL}/api/health/ready`);
        stopped = false;
        await page.getByRole('button', { name: 'Try again' }).click();
        await expect(page.locator('.mf-thread')).toHaveCount(20);
        await page.getByRole('button', { name: 'Close discussions' }).click();
        await expect(page.locator('[data-discussion-count]').first()).toHaveText('23');
        await page.getByRole('button', { name: 'Retry counts' }).click();
        await expect(page.locator('[data-discussion-count]').nth(1)).toHaveText('0');
        await expect(page.locator('[data-counts-status]')).toBeHidden();
    } finally {
        if (stopped) {
            compose('start', 'forum-engine');
            await waitUntilReady(request, `${forumURL}/api/health/ready`);
        }
    }
});

test('index outage preserves the query and the retry link recovers', async ({ page, request }, testInfo) => {
    let stopped = false;
    try {
        compose('stop', '--timeout', '5', 'mongo');
        stopped = true;
        const response = await page.goto('/search/exact?q=staging');
        expect(response.status()).toBe(503);
        await expect(page.getByRole('heading', { name: 'Search is temporarily unavailable' })).toBeVisible();
        await expect(page.getByRole('searchbox')).toHaveValue('staging');
        await page.screenshot({ path: testInfo.outputPath('index-unavailable.png') });
        const webRetry = await page.getByRole('link', { name: 'Try again', exact: true }).getAttribute('href');
        const images = await page.goto('/images?q=ux06&page=2');
        expect(images.status()).toBe(503);
        await expect(page.getByRole('searchbox', { name: 'Search images' })).toHaveValue('ux06');
        const imageRetry = await page.getByRole('link', { name: 'Try again', exact: true }).getAttribute('href');
        await page.screenshot({ path: testInfo.outputPath('images-unavailable.png') });
        const links = await page.goto(`/page-links?${new URLSearchParams({ url: sourceURL, direction: 'incoming', page: '2', from: '/search/exact?q=staging' })}`);
        expect(links.status()).toBe(503);
        await expect(page.getByRole('textbox', { name: 'Explore a page’s connections' })).toHaveValue(sourceURL);
        const linksRetry = await page.getByRole('link', { name: 'Try again', exact: true }).getAttribute('href');
        await page.screenshot({ path: testInfo.outputPath('page-links-unavailable.png') });
        compose('start', 'mongo');
        await waitUntilReady(request, '/api/health/ready');
        stopped = false;
        // Readiness can recover on one FPM worker before another worker's MongoDB
        // topology has cleared its failure cooldown. Exercise the real retry link
        // within a bounded recovery window, retaining its exact destination.
        const followRetry = async url => {
            await expect(async () => {
                const retry = page.getByRole('link', { name: 'Try again', exact: true });
                await expect(retry).toHaveAttribute('href', url);
                const [response] = await Promise.all([page.waitForNavigation(), retry.click()]);
                expect(response.status()).toBe(200);
            }).toPass({ timeout: 15000, intervals: [500, 1000] });
        };
        await followRetry(linksRetry);
        await expect(page.locator('.mf-connection-list li')).toHaveCount(5);
        expect(new URL(page.url()).searchParams.get('direction')).toBe('incoming');
        expect(new URL(page.url()).searchParams.get('from')).toBe('/search/exact?q=staging');
        const imageResponse = await page.goto(imageRetry);
        if (imageResponse.status() === 503) await followRetry(imageRetry);
        else expect(imageResponse.status()).toBe(200);
        await expect(page.locator('.mf-image-card')).toHaveCount(5);
        const webResponse = await page.goto(webRetry);
        if (webResponse.status() === 503) await followRetry(webRetry);
        else expect(webResponse.status()).toBe(200);
        await expect(page.locator('.mf-result-card')).toHaveCount(20);
    } finally {
        if (stopped) {
            compose('start', 'mongo');
            await waitUntilReady(request, '/api/health/ready');
        }
    }
});
