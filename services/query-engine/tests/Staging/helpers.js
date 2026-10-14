import { setTimeout as wait } from 'node:timers/promises';
import { test, expect } from '@playwright/test';

export const forumURL = 'http://127.0.0.1:18082';
export const sourceURL = 'https://developer.mozilla.org/en-US/docs/Web/API/URLSearchParams?topic=search&lang=en';

export async function assertStaging(request) {
    for (const url of ['/api/health/ready', `${forumURL}/api/health/ready`]) {
        const response = await request.get(url);
        expect(response.ok(), `${url} must be ready`).toBe(true);
        expect(response.headers()['x-mifolyo-environment']).toBe('local-search-staging');
        expect(response.headers()['x-mifolyo-browser-fixture']).toBeUndefined();
    }
}

export async function waitUntilReady(request, url) {
    await expect.poll(async () => {
        try { return (await request.get(url, { timeout: 5000 })).status(); }
        catch { return 0; }
    }, { timeout: 30000, intervals: [500, 1000, 2000] }).toBe(200);
}

export async function signIn(page) {
    const returnTo = new URL(page.url()).searchParams.get('return_to');
    for (let attempt = 0; attempt < 2; attempt++) {
        await page.getByLabel('Email', { exact: true }).fill('reader@staging.mifolyo.test');
        await page.getByLabel('Password', { exact: true }).fill('local-staging-only-password');
        const submitted = page.waitForResponse(response => new URL(response.url()).pathname === '/sign-in' && response.request().method() === 'POST');
        await page.getByRole('button', { name: 'Sign in', exact: true }).click();
        const response = await submitted;
        if (response.status() !== 429 || attempt === 1) {
            expect(response.status()).toBe(302);
            await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible();
            return;
        }
        // The expanded account journeys can reach the real 10/minute limit.
        // Follow its advertised backoff and verify that retry retains the destination.
        const retry = page.getByRole('link', { name: 'Try again', exact: true });
        await expect(retry).toBeVisible();
        expect(new URL(await retry.getAttribute('href')).searchParams.get('return_to')).toBe(returnTo);
        const seconds = Number(response.headers()['retry-after']);
        expect(seconds).toBeGreaterThanOrEqual(1);
        expect(seconds).toBeLessThanOrEqual(60);
        const delay = (seconds + 1) * 1000;
        test.info().setTimeout(test.info().timeout + delay);
        await wait(delay);
        await retry.click();
    }
}
