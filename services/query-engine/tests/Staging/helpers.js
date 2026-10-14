import { expect } from '@playwright/test';

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
