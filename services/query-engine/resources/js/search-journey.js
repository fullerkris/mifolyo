// URLs carry the reading destination; tab-local state adds scroll and panel restoration.
// A plain link still returns to the exact query/page when storage or JavaScript is unavailable.
export function initSearchJourney(root, dialog, panel) {
    if (!root) return;
    const from = root.dataset.searchContext;
    const links = [...root.querySelectorAll('[data-discuss-url]')];
    const storageKey = 'mifolyo-search-journeys';
    let selected;
    let scroll = window.scrollY;
    let restoring = false;
    const stored = () => {
        try {
            const data = JSON.parse(sessionStorage.getItem(storageKey) || '[]');
            return Array.isArray(data) ? data : [];
        } catch (_) { return []; }
    };
    const remember = () => {
        if (!from || restoring) return;
        const state = {
            url: from, source: selected?.dataset.discussUrl,
            scroll: dialog?.open ? scroll : window.scrollY, panel: Boolean(dialog?.open),
            sort: panel?.sort.value, page: panel?.page, savedAt: Date.now(),
        };
        try { history.replaceState({ ...history.state, mfSearch: state }, ''); } catch (_) { /* Navigation still works. */ }
        try { sessionStorage.setItem(storageKey, JSON.stringify([...stored().filter(item => item?.url !== from).slice(-9), state])); } catch (_) { /* History is enough for browser Back. */ }
    };
    const canOpenPanel = () => dialog && panel && !matchMedia('(max-width: 768px)').matches && typeof dialog.showModal === 'function';
    const open = (link, state = {}) => {
        selected = link;
        dialog.querySelector('[data-panel-title]').textContent = link.dataset.discussTitle;
        const source = dialog.querySelector('[data-panel-source]');
        source.textContent = link.dataset.discussUrl;
        source.href = link.dataset.discussUrl;
        dialog.querySelector('[data-panel-page]').href = link.href;
        if (!dialog.open) dialog.showModal();
        panel.setSource(link.dataset.discussUrl, state);
    };
    for (const link of links) {
        // Also capture the context for opening a discussion in another tab.
        link.addEventListener('pointerdown', () => { selected = link; scroll = window.scrollY; remember(); });
        link.addEventListener('click', event => {
            selected = link;
            scroll = window.scrollY;
            if (event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && canOpenPanel()) {
                event.preventDefault();
                open(link);
            }
            remember();
        });
    }
    if (dialog && panel) {
        dialog.querySelector('[data-close-discussions]').addEventListener('click', () => dialog.close());
        dialog.addEventListener('click', event => {
            const rect = dialog.getBoundingClientRect();
            if (event.target === dialog && (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom)) dialog.close();
        });
        dialog.addEventListener('close', () => {
            panel.cancel();
            selected?.focus({ preventScroll: true });
            remember();
        });
        panel.root.addEventListener('threads:state', remember);
    }
    window.addEventListener('pagehide', remember);
    const restore = () => {
        const explicitReturn = location.hash === '#mf-return';
        const state = history.state?.mfSearch ?? (explicitReturn ? stored().find(item => item?.url === from) : null);
        if (!state || state.url !== from || !Number.isFinite(state.savedAt) || Date.now() - state.savedAt > 7200000) return;
        restoring = true;
        selected = links.find(link => link.dataset.discussUrl === state.source);
        scroll = Number.isFinite(state.scroll) ? Math.max(0, Math.min(10000000, state.scroll)) : 0;
        if (state.panel && selected && canOpenPanel()) open(selected, state);
        else selected?.focus({ preventScroll: true });
        window.scrollTo({ top: scroll, behavior: 'instant' });
        // Apply once after layout/browser restoration; later scrolling remains under the reader's control.
        requestAnimationFrame(() => window.scrollTo({ top: scroll, behavior: 'instant' }));
        if (explicitReturn) history.replaceState({ ...history.state, mfSearch: state }, '', location.pathname + location.search);
        restoring = false;
    };
    window.addEventListener('pageshow', restore);
    if (document.readyState === 'complete') restore();
}
