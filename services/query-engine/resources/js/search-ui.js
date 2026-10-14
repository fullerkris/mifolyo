import { ThreadBrowser } from './thread-browser.js';
import { initSearchJourney } from './search-journey.js';
import { initDiscussionCounts } from './discussion-counts.js';

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
const results = document.querySelector('[data-search-context]');
initDiscussionCounts(results);
initSearchJourney(results, dialog, panel);
document.querySelectorAll('[data-thread-browser]').forEach((root) => {
    if (root !== panelRoot && root.dataset.sourceUrl) new ThreadBrowser(root).setSource(root.dataset.sourceUrl);
});
