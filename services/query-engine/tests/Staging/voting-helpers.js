import { randomUUID } from 'node:crypto';
import { expect } from '@playwright/test';
import { navigate, signIn } from './helpers.js';

export const voteGroup = (page, type, id) => page.locator(`[data-vote-type="${type}"][data-vote-id="${id}"]`);
export const votingOrigin = '/images?q=staging%20%26%20caf%C3%A9%20C%2B%2B&page=2';

export async function createVotingConversation(page, noScript = false, authenticate = signIn) {
    const source = `https://example.test/voting/${randomUUID()}?b=2&a=1`;
    await navigate(page, () => page.goto(`/discussions?${new URLSearchParams({ url: source, from: votingOrigin })}`));
    await page.getByRole('link', { name: 'Sign in to start a thread' }).click();
    await authenticate(page);
    await page.locator('.mf-create-thread summary').click();
    await page.getByLabel('Discussion title').fill('A local staging voting conversation');
    await page.getByLabel('Body optional').fill('A fresh conversation for persisted thread and comment voting checks.');
    await page.getByRole('button', { name: 'Start thread', exact: true }).click();
    if (!noScript) await page.getByRole('link', { name: 'Open your conversation →' }).click();
    await expect(page.getByRole('heading', { name: 'A local staging voting conversation', exact: true })).toBeVisible();
    const thread = Number(new URL(page.url()).pathname.split('/').pop());
    await page.getByLabel('Your reply').fill('A voting test root comment.');
    await page.getByRole('button', { name: 'Post reply', exact: true }).click();
    const root = Number(new URL(page.url()).hash.replace('#comment-', ''));
    await page.locator('.mf-comment > article').first().getByRole('link', { name: /^Reply/ }).click();
    await page.getByLabel('Your reply').fill('A nested voting test comment.');
    await page.getByRole('button', { name: 'Post reply', exact: true }).click();
    const child = Number(new URL(page.url()).hash.replace('#comment-', ''));
    return { thread, root, child, source };
}
