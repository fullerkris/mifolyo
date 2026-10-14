export function initDiscussionCounts(root) {
    if (!root) return;
    const links = [...root.querySelectorAll('[data-discuss-url]')];
    if (!links.length) return;
    const urls = [...new Set(links.map(link => link.dataset.discussUrl))];
    const status = root.querySelector('[data-counts-status]');
    const retry = root.querySelector('[data-counts-retry]');
    let controller;
    let generation = 0;
    let freshCounts = new Map();
    const display = (link, count, loading = false) => {
        const badge = link.querySelector('[data-discussion-count]');
        badge.hidden = false;
        badge.textContent = loading ? '…' : count === null ? '—' : Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(count);
        badge.title = loading ? 'Loading conversation count' : count === null ? 'Conversation count unavailable'
            : count === 0 ? 'No conversations yet — start the first' : `${count.toLocaleString()} ${count === 1 ? 'conversation' : 'conversations'}`;
        badge.setAttribute('aria-label', badge.title);
    };
    const load = async () => {
        controller?.abort();
        const requestController = new AbortController();
        controller = requestController;
        const current = ++generation;
        freshCounts = new Map();
        const timer = setTimeout(() => requestController.abort(), 12000);
        retry.disabled = true;
        status.hidden = true;
        links.forEach(link => display(link, null, true));
        try {
            const response = await fetch(root.dataset.countsEndpoint, {
                method: 'POST', signal: requestController.signal,
                headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-TOKEN': document.querySelector('meta[name="csrf-token"]').content },
                body: JSON.stringify({ urls }),
            });
            if (!response.ok) throw new Error('Counts unavailable');
            const payload = await response.json();
            if (current !== generation) return;
            if (!Array.isArray(payload.data) || payload.data.length !== urls.length || payload.data.some((item, index) =>
                item?.url !== urls[index] || !(item.count === null || (Number.isSafeInteger(item.count) && item.count >= 0)))) throw new Error('Invalid counts');
            const counts = new Map(payload.data.map(item => [item.url, item.count]));
            for (const [source, total] of freshCounts) counts.set(source, total);
            links.forEach(link => display(link, counts.get(link.dataset.discussUrl)));
            status.hidden = urls.every(url => counts.get(url) !== null);
        } catch (_) {
            if (current !== generation) return;
            links.forEach(link => display(link, freshCounts.get(link.dataset.discussUrl) ?? null));
            status.hidden = urls.every(url => freshCounts.has(url));
        } finally {
            clearTimeout(timer);
            if (current === generation) retry.disabled = false;
        }
    };
    retry.addEventListener('click', load);
    document.addEventListener('threads:loaded', event => {
        // The current source list is fresher than a batch begun before a thread was created.
        freshCounts.set(event.detail.source, event.detail.total);
        for (const link of links) if (link.dataset.discussUrl === event.detail.source) display(link, event.detail.total);
    });
    links.forEach(link => display(link, null, true));
    window.addEventListener('pageshow', load);
    window.addEventListener('pagehide', () => { generation++; controller?.abort(); });
    if (document.readyState === 'complete') load();
}
