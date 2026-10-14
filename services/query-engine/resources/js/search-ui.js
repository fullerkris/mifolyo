import { ThreadBrowser } from './thread-browser.js';

const themeButton = document.querySelector('[data-theme-toggle]');
const updateThemeButton = () => {
    const dark = document.documentElement.dataset.theme === 'dark';
    themeButton?.setAttribute('aria-pressed', String(dark));
    themeButton?.setAttribute('aria-label', `Switch to ${dark ? 'light' : 'dark'} mode`);
};
if (themeButton) {
    themeButton.hidden = false;
    updateThemeButton();
    themeButton.addEventListener('click', () => {
        const theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
        document.documentElement.dataset.theme = theme;
        try { localStorage.setItem('mifolyo-theme', theme); } catch (_) { /* Theme still works for this page. */ }
        updateThemeButton();
    });
}

const navigationStatus = document.querySelector('[data-navigation-status]');
document.querySelectorAll('[data-posting-form]').forEach((form) => {
    form.addEventListener('submit', () => {
        form.setAttribute('aria-busy', 'true');
        form.querySelector('button[type="submit"]').disabled = true;
    });
});
document.querySelectorAll('[data-search-form]').forEach((form) => {
    form.addEventListener('submit', () => {
        form.setAttribute('aria-busy', 'true');
        if (navigationStatus) navigationStatus.hidden = false;
    });
});
window.addEventListener('pageshow', () => {
    document.querySelectorAll('[data-posting-form]').forEach((form) => {
        form.removeAttribute('aria-busy');
        form.querySelector('button[type="submit"]').disabled = false;
    });
    document.querySelectorAll('[data-search-form]').forEach((form) => form.removeAttribute('aria-busy'));
    if (navigationStatus) navigationStatus.hidden = true;
    updateThemeButton();
});

const dialog = document.querySelector('[data-discussion-dialog]');
const panelRoot = dialog?.querySelector('[data-thread-browser]');
const panel = panelRoot ? new ThreadBrowser(panelRoot) : null;
let opener;
if (dialog && panel) {
    document.querySelectorAll('[data-discuss-url]').forEach((link) => {
        link.addEventListener('click', (event) => {
            if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey
                || matchMedia('(max-width: 768px)').matches || typeof dialog.showModal !== 'function') return;
            event.preventDefault();
            opener = link;
            dialog.querySelector('[data-panel-title]').textContent = link.dataset.discussTitle;
            const source = dialog.querySelector('[data-panel-source]');
            source.textContent = link.dataset.discussUrl;
            source.href = link.dataset.discussUrl;
            dialog.querySelector('[data-panel-page]').href = link.href;
            dialog.showModal();
            panel.setSource(link.dataset.discussUrl);
        });
    });
    dialog.querySelector('[data-close-discussions]').addEventListener('click', () => dialog.close());
    dialog.addEventListener('click', (event) => {
        const rect = dialog.getBoundingClientRect();
        if (event.target === dialog && (event.clientX < rect.left || event.clientX > rect.right
            || event.clientY < rect.top || event.clientY > rect.bottom)) dialog.close();
    });
    dialog.addEventListener('close', () => {
        panel.cancel();
        opener?.focus();
    });
}
document.querySelectorAll('[data-thread-browser]').forEach((root) => {
    if (root !== panelRoot && root.dataset.sourceUrl) new ThreadBrowser(root).setSource(root.dataset.sourceUrl);
});
