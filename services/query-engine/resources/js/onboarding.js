// Account-backed progress; browser storage is not required or shared between readers.
export function initOnboarding() {
    const { onboardingUser: user, onboardingEndpoint: endpoint } = document.body.dataset;
    const hints = [...document.querySelectorAll('[data-onboarding-step]')];
    if (!user || !endpoint || !hints.length) return;
    const pending = new Set();
    const hide = step => {
        for (const hint of hints.filter(item => item.dataset.onboardingStep === step)) {
            const focused = hint.contains(document.activeElement);
            hint.hidden = true;
            if (hint.id) document.querySelectorAll(`[aria-describedby="${hint.id}"]`).forEach(item => item.removeAttribute('aria-describedby'));
            if (focused) (document.querySelector('[data-search-form] input[name="q"]') || document.querySelector('#main'))?.focus({ preventScroll: true });
        }
    };
    const apply = payload => {
        if (String(payload.user_id) !== user) {
            hints.forEach(hint => hide(hint.dataset.onboardingStep));
            return;
        }
        for (const step of ['welcome', 'discuss', 'vote']) {
            if (payload.data?.[step] === false) hide(step);
        }
    };
    const request = async options => {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 10000);
        try {
            const response = await fetch(endpoint, { credentials: 'same-origin', cache: 'no-store', ...options, signal: controller.signal });
            if (response.status === 401 || response.status === 409) hints.forEach(hint => hide(hint.dataset.onboardingStep));
            if (!response.ok) throw new Error('Onboarding unavailable');
            const payload = await response.json();
            if (!payload.data || !['welcome', 'discuss', 'vote'].every(step => typeof payload.data[step] === 'boolean')) throw new Error('Invalid onboarding response');
            apply(payload);
        } finally {
            clearTimeout(timer);
        }
    };
    const complete = async step => {
        if (pending.has(step)) return;
        pending.add(step);
        try {
            await request({
                method: 'POST',
                headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-TOKEN': document.querySelector('meta[name="csrf-token"]').content },
                body: JSON.stringify({ step, user_id: user }),
            });
        } finally {
            pending.delete(step);
        }
    };
    const form = document.querySelector('[data-onboarding-dismiss]');
    form?.addEventListener('submit', async event => {
        event.preventDefault();
        if (pending.has('welcome')) return;
        const button = form.querySelector('button');
        const status = form.querySelector('[data-onboarding-status]');
        form.setAttribute('aria-busy', 'true');
        button.setAttribute('aria-disabled', 'true');
        status.textContent = 'Saving…';
        try {
            await complete('welcome');
            status.textContent = '';
        } catch (_) {
            status.textContent = 'We couldn’t save your progress. Choose Got it to try again.';
        } finally {
            form.removeAttribute('aria-busy');
            button.removeAttribute('aria-disabled');
        }
    });
    document.addEventListener('threads:loaded', () => {
        if (hints.some(hint => hint.dataset.onboardingStep === 'discuss' && !hint.hidden)) {
            // A failed save leaves the hint available for a later successful visit.
            complete('discuss').catch(() => {});
        }
    });
    document.addEventListener('mifolyo:vote-saved', event => {
        if (event.detail.value === 1 || event.detail.value === -1) hide('vote');
    });
    let refreshing = false;
    const refresh = async () => {
        if (refreshing || document.visibilityState === 'hidden') return;
        refreshing = true;
        try { await request({ headers: { Accept: 'application/json' } }); } catch (_) { /* Keep reading available during an outage. */ }
        finally { refreshing = false; }
    };
    window.addEventListener('pageshow', event => { if (event.persisted) refresh(); });
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
}
