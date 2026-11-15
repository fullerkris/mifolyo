// Every submission sets an explicit vote. Retrying an uncertain write never toggles it.
export function initVoteControls(root) {
    const form = root.querySelector('[data-vote-form]');
    const buttons = [...root.querySelectorAll('[data-vote-direction]')];
    if (!form || buttons.every(button => button.disabled)) return;
    const score = root.querySelector('[data-vote-score]');
    const status = root.querySelector('[data-vote-status]');
    const retry = root.querySelector('[data-vote-retry]');
    const signIn = root.querySelector('[data-vote-sign-in]');
    const reload = root.querySelector('[data-vote-reload]');
    let busy = false;
    let blocked = false;
    let retryAt = 0;
    let countdown;
    const controls = () => {
        // Keep keyboard focus on the pressed control while a request is pending.
        const unavailable = String(busy || blocked || Date.now() < retryAt);
        buttons.forEach(button => button.setAttribute('aria-disabled', unavailable));
        retry.setAttribute('aria-disabled', unavailable);
        form.setAttribute('aria-busy', String(busy));
    };
    const announce = (message, error = false) => {
        status.textContent = message;
        status.classList.toggle('mf-vote-error', error);
    };
    document.addEventListener('mifolyo:vote-session-expired', () => {
        blocked = true;
        clearInterval(countdown);
        retry.hidden = true;
        signIn.hidden = false;
        controls();
    });
    form.addEventListener('submit', async event => {
        event.preventDefault();
        if (busy || blocked || Date.now() < retryAt || !event.submitter) return;
        const value = Number(event.submitter.value);
        if (![-1, 0, 1].includes(value)) return;
        const body = new FormData(form);
        body.set('value', String(value));
        busy = true;
        if (event.submitter !== retry) retry.hidden = true;
        reload.hidden = true;
        controls();
        announce('Saving vote…');
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), 15000);
        try {
            const response = await fetch(form.action, { method: 'POST', body, credentials: 'same-origin', headers: { Accept: 'application/json' }, signal: controller.signal });
            // Another control may have discovered an expired session during this request.
            if (blocked) return;
            if (response.status === 401) {
                document.dispatchEvent(new Event('mifolyo:vote-session-expired'));
                announce('Please sign in again to vote. You’ll return to this conversation.', true);
                return;
            }
            if ([403, 404, 419, 422, 423].includes(response.status)) {
                blocked = true;
                reload.hidden = false;
                announce({
                    403: 'You no longer have access to vote in this conversation.',
                    404: 'This thread or comment is no longer available.',
                    419: 'Your page has expired. Reload the conversation before voting again.',
                    422: 'This vote could not be saved. Reload the conversation and try again.',
                    423: 'Voting is closed for this thread or comment.',
                }[response.status], true);
                return;
            }
            if (response.status === 429) {
                const seconds = Number(response.headers.get('Retry-After'));
                retryAt = Date.now() + (Number.isInteger(seconds) && seconds > 0 && seconds <= 3600 ? seconds : 60) * 1000;
                retry.value = String(value);
                retry.hidden = false;
                const tick = () => {
                    const remaining = Math.max(0, Math.ceil((retryAt - Date.now()) / 1000));
                    announce(remaining ? `Too many votes. Try again in ${remaining} seconds.` : 'You can retry your vote now.', Boolean(remaining));
                    controls();
                    if (!remaining) clearInterval(countdown);
                };
                clearInterval(countdown);
                countdown = setInterval(tick, 1000);
                tick();
                return;
            }
            if (!response.ok) throw new Error('Vote unavailable');
            const { data } = await response.json();
            if (!data || data.type !== root.dataset.voteType || data.id !== Number(root.dataset.voteId)
                || !Number.isSafeInteger(data.score) || data.viewer_vote !== value) throw new Error('Invalid vote response');
            // A different vote request may have discovered an expired session meanwhile.
            if (blocked) return;
            root.dataset.currentVote = String(value);
            score.textContent = new Intl.NumberFormat('en').format(data.score);
            buttons.forEach(button => {
                const selected = Number(button.dataset.voteDirection) === value;
                button.setAttribute('aria-pressed', String(selected));
                button.value = selected ? '0' : button.dataset.voteDirection;
            });
            if (document.activeElement === retry) {
                buttons.find(button => Number(button.dataset.voteDirection) === (value || 1))?.focus({ preventScroll: true });
            }
            retry.hidden = true;
            document.dispatchEvent(new CustomEvent('mifolyo:vote-saved', { detail: { value } }));
            announce(`${value === 0 ? 'Vote removed.' : value === 1 ? 'Upvote saved.' : 'Downvote saved.'} Score ${data.score}.`);
        } catch (_) {
            if (!blocked) {
                retry.value = String(value);
                retry.hidden = false;
                announce('We couldn’t confirm your vote. Retry to save the same choice.', true);
            }
        } finally {
            clearTimeout(timeout);
            busy = false;
            controls();
        }
    });
}
