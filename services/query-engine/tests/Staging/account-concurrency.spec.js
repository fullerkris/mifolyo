import { test, expect } from '@playwright/test';
import { assertStaging } from './helpers.js';
import { api, createAccount, createAccountThread, password, resetMailLink } from './account-helpers.js';

test('PostgreSQL serializes reset consumption and concurrent deletion/voting', async ({ request }) => {
    await assertStaging(request);
    const account = await createAccount(request);
    const other = await createAccount(request);
    const { thread } = await createAccountThread(request, account);
    expect((await api(request, 'post', '/auth/forgot-password', { email: account.email })).status()).toBe(202);
    const link = new URL(await resetMailLink(request, account.email));
    const replacement = 'replacement-concurrent-password';
    const credentials = { email: account.email, token: link.searchParams.get('token'), password: replacement, password_confirmation: replacement };
    const resets = await Promise.all([api(request, 'post', '/auth/reset-password', credentials), api(request, 'post', '/auth/reset-password', credentials)]);
    expect(resets.map(response => response.status()).sort()).toEqual([200, 422]);
    expect((await api(request, 'post', '/auth/refresh', {}, account.token)).status()).toBe(401);
    expect((await api(request, 'post', '/auth/login', { email: account.email, password })).status()).toBe(422);
    const login = await api(request, 'post', '/auth/login', { email: account.email, password: replacement });
    expect(login.ok()).toBe(true);
    const token = (await login.json()).data.token;
    expect((await api(request, 'post', '/votes', { votable_type: 'post', votable_id: thread, value: 1 }, token)).ok()).toBe(true);
    const [deleted, ownerVote, otherVote] = await Promise.all([
        api(request, 'delete', '/auth/account', { password: replacement, confirmation: true }, token),
        api(request, 'post', '/votes', { votable_type: 'post', votable_id: thread, value: -1 }, token),
        api(request, 'post', '/votes', { votable_type: 'post', votable_id: thread, value: 1 }, other.token),
    ]);
    expect(deleted.status()).toBe(200);
    expect([200, 401]).toContain(ownerVote.status());
    expect(otherVote.status()).toBe(200);
    expect((await (await api(request, 'get', `/posts/${thread}`)).json()).data)
        .toMatchObject({ author: null, score: 1, upvote_count: 1, downvote_count: 0 });
    expect((await api(request, 'post', '/auth/refresh', {}, token)).status()).toBe(401);
});
