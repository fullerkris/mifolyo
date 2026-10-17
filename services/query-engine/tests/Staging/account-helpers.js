import { randomUUID } from 'node:crypto';
import { setTimeout as wait } from 'node:timers/promises';
import { test, expect } from '@playwright/test';
import { forumURL } from './helpers.js';

export const mailURL = 'http://127.0.0.1:18085';
export const password = 'local-account-test-password';

async function backoff(response) {
    const seconds = Number(response.headers()['retry-after']);
    expect(seconds).toBeGreaterThanOrEqual(1);
    expect(seconds).toBeLessThanOrEqual(60);
    test.info().setTimeout(test.info().timeout + (seconds + 1) * 1000);
    await wait((seconds + 1) * 1000);
}

export async function api(request, method, path, data, token) {
    for (let attempt = 0; attempt < 2; attempt++) {
        const response = await request[method](`${forumURL}/api${path}`, {
            data, headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (response.status() !== 429 || attempt) return response;
        await backoff(response);
    }
}

export async function createAccount(request) {
    const username = `account_${randomUUID().replaceAll('-', '').slice(0, 12)}`;
    const email = `${username}@staging.mifolyo.test`;
    const response = await api(request, 'post', '/auth/register', {
        name: 'Account flow reader', username, email, password, password_confirmation: password,
    });
    expect(response.status()).toBe(201);
    return { email, username, ...(await response.json()).data };
}

export async function createAccountThread(request, account) {
    const response = await api(request, 'post', '/threads', {
        community_slug: 'general', title: 'An account lifecycle conversation', body: 'Keep this discussion after account deletion.',
        source_url: `https://example.test/account/${account.username}?b=2&a=1`,
    }, account.token);
    expect(response.status()).toBe(201);
    const thread = (await response.json()).data.id;
    const commentResponse = await api(request, 'post', `/posts/${thread}/comments`, { body: 'Keep this reply too.' }, account.token);
    expect(commentResponse.status()).toBe(201);
    return { thread, comment: (await commentResponse.json()).data.id };
}

export async function submitAccount(page, button, fill) {
    for (let attempt = 0; attempt < 2; attempt++) {
        await fill();
        const responsePromise = page.waitForResponse(response => response.request().isNavigationRequest() && response.request().method() === 'POST');
        await page.getByRole('button', { name: button, exact: true }).click();
        const response = await responsePromise;
        if (response.status() !== 429 || attempt) {
            expect(response.status()).toBe(302);
            return;
        }
        const retry = page.getByRole('link', { name: /^(Try again|Return to form)$/ });
        await expect(retry).toBeVisible();
        await backoff(response);
        await retry.click();
    }
}

export async function accountSignIn(page, email, currentPassword = password) {
    await submitAccount(page, 'Sign in', async () => {
        await page.getByLabel('Email', { exact: true }).fill(email);
        await page.getByLabel('Password', { exact: true }).fill(currentPassword);
    });
    await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible();
}

export async function resetMailLink(request, email) {
    let message;
    await expect.poll(async () => {
        const response = await request.get(`${mailURL}/api/v1/messages`);
        expect(response.ok()).toBe(true);
        message = (await response.json()).messages.find(item => item.To.some(recipient => recipient.Address === email));
        return Boolean(message);
    }, { timeout: 10000 }).toBe(true);
    const response = await request.get(`${mailURL}/api/v1/message/${message.ID}`);
    const text = (await response.json()).Text;
    const link = text.match(/http:\/\/127\.0\.0\.1:18081\/reset-password\?[^\s)<>]+/);
    expect(link).not.toBeNull();
    return link[0];
}
