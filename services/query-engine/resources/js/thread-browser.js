const textElement = (tag, className, text) => {
    const element = document.createElement(tag);
    element.className = className;
    element.textContent = text;
    return element;
};
const count = (value) => Number.isSafeInteger(value) ? value : 0;

export class ThreadBrowser {
    constructor(root) {
        this.root = root;
        this.status = root.querySelector('[data-thread-status]');
        this.list = root.querySelector('[data-thread-list]');
        this.retry = root.querySelector('[data-thread-retry]');
        this.sort = root.querySelector('[data-thread-sort]');
        this.pagination = root.querySelector('[data-thread-pagination]');
        this.previous = root.querySelector('[data-thread-previous]');
        this.next = root.querySelector('[data-thread-next]');
        this.form = root.querySelector('[data-thread-create]');
        this.createStatus = root.querySelector('[data-create-status]');
        this.generation = 0;
        this.sourceGeneration = 0;
        this.page = 1;
        this.source = '';
        root.querySelector('[data-thread-sort-label]').hidden = false;
        this.sort.addEventListener('change', () => this.load(1));
        this.retry.addEventListener('click', () => this.load(this.page));
        this.previous.addEventListener('click', () => this.load(this.page - 1));
        this.next.addEventListener('click', () => this.load(this.page + 1));
        this.form?.addEventListener('submit', (event) => {
            event.preventDefault();
            this.create();
        });
    }

    cancel() {
        this.generation++;
        this.sourceGeneration++;
        this.controller?.abort();
        this.createController?.abort();
        this.root.removeAttribute('aria-busy');
    }

    link(path) {
        const url = new URL(path, location.origin);
        if (this.root.dataset.searchReturn) url.searchParams.set('from', this.root.dataset.searchReturn);
        return url.pathname + url.search + url.hash;
    }

    setSource(source, state = {}) {
        this.cancel();
        this.source = source;
        this.sort.value = state.sort === 'new' ? 'new' : 'top';
        if (this.form) {
            this.form.reset();
            this.form.elements.source_url.value = source;
            this.form.querySelector('button').disabled = false;
            this.createStatus.textContent = '';
            this.root.querySelector('.mf-create-thread').open = source === this.root.dataset.sourceUrl
                && (this.form.elements.title.value.trim() !== '' || Boolean(this.root.querySelector('.mf-form-error')));
        }
        const returnTo = this.link(`/discussions?${new URLSearchParams({ url: source })}`);
        this.root.querySelector('[data-forum-sign-in]').href = `/sign-in?${new URLSearchParams({ return_to: returnTo })}`;
        this.root.querySelector('[data-forum-register]').href = `/register?${new URLSearchParams({ return_to: returnTo })}`;
        this.load(Number.isSafeInteger(state.page) ? state.page : 1);
    }

    async load(page) {
        this.controller?.abort();
        const controller = new AbortController();
        this.controller = controller;
        const generation = ++this.generation;
        this.page = Math.max(1, Math.min(10000, page));
        this.root.dispatchEvent(new CustomEvent('threads:state', { bubbles: true }));
        const url = new URL(this.root.dataset.threadsEndpoint, location.origin);
        url.search = new URLSearchParams({ url: this.source, sort: this.sort.value, page: this.page, per_page: 20 });
        this.list.replaceChildren();
        this.status.textContent = 'Loading community threads…';
        this.root.setAttribute('aria-busy', 'true');
        this.retry.hidden = true;
        this.pagination.hidden = true;
        const timer = setTimeout(() => controller.abort(), 12000);
        try {
            const response = await fetch(url, { headers: { Accept: 'application/json' }, signal: controller.signal });
            if (!response.ok) throw new Error(response.status === 429
                ? 'Too many requests. Wait a moment, then try again.' : 'Forum threads are unavailable right now. Please try again.');
            const payload = await response.json();
            // Check after reading the body too: an older response can finish parsing after a new selection.
            if (generation !== this.generation) return;
            if (!Array.isArray(payload.data) || payload.data.some((thread) => !thread || typeof thread.title !== 'string')) {
                throw new Error('The discussion service returned an unexpected response. Please try again.');
            }
            const meta = payload.meta ?? {};
            const currentPage = meta.current_page ?? this.page;
            const lastPage = meta.last_page ?? this.page;
            const total = meta.total ?? payload.data.length;
            if (!Number.isSafeInteger(currentPage) || currentPage !== this.page
                || !Number.isSafeInteger(lastPage) || lastPage < 1
                || !Number.isSafeInteger(total) || total < 0) throw new Error('Could not read the discussion pages. Please try again.');
            this.render(payload.data);
            this.root.dispatchEvent(new CustomEvent('threads:loaded', { bubbles: true, detail: { source: this.source, total } }));
            this.status.textContent = total === 0 ? 'No discussions yet for this page. A good question can start a conversation.'
                : `${total.toLocaleString()} ${total === 1 ? 'conversation' : 'conversations'} for this page`;
            this.pagination.hidden = lastPage <= 1;
            this.previous.disabled = currentPage <= 1;
            this.next.disabled = currentPage >= Math.min(10000, lastPage);
            this.root.querySelector('[data-thread-page]').textContent = `Page ${currentPage} of ${lastPage}`;
        } catch (error) {
            if (generation !== this.generation) return;
            this.status.textContent = error.name === 'AbortError' ? 'The discussion service took too long to respond. Please try again.'
                : error instanceof TypeError || error instanceof SyntaxError ? 'Could not reach the discussion service. Please try again.' : error.message;
            this.retry.hidden = false;
        } finally {
            clearTimeout(timer);
            if (generation === this.generation) this.root.removeAttribute('aria-busy');
        }
    }

    render(threads) {
        for (const thread of threads) {
            const article = textElement('article', 'mf-thread', '');
            if (typeof thread.community?.name === 'string') article.append(textElement('p', 'mf-eyebrow', thread.community.name));
            const heading = textElement('h3', '', thread.title);
            if (Number.isSafeInteger(thread.id) && thread.id > 0) {
                const link = textElement('a', '', thread.title);
                link.href = this.link(`/threads/${thread.id}`);
                heading.replaceChildren(link);
            }
            article.append(heading);
            const author = typeof thread.author?.username === 'string' ? thread.author.username : 'Community member';
            const replies = count(thread.comment_count);
            article.append(textElement('p', 'mf-thread-meta', `${author} · ${replies} ${replies === 1 ? 'reply' : 'replies'} · ${count(thread.score)} score`));
            if (typeof thread.body === 'string' && thread.body.trim()) {
                const body = textElement('p', 'mf-thread-body', thread.body);
                if (thread.body.length > 350) {
                    const details = document.createElement('details');
                    details.append(textElement('summary', '', 'Read discussion'), body);
                    article.append(details);
                } else article.append(body);
            }
            if (Number.isSafeInteger(thread.id) && thread.id > 0) {
                const link = textElement('a', 'mf-text-link mf-thread-open', 'Read conversation →');
                link.href = this.link(`/threads/${thread.id}`);
                article.append(link);
            }
            this.list.append(article);
        }
    }

    async create() {
        const button = this.form.querySelector('button');
        if (!this.source || button.disabled) return;
        const sourceGeneration = this.sourceGeneration;
        const fields = new FormData(this.form);
        const title = String(fields.get('title') ?? '').trim();
        if (!title) {
            this.createStatus.textContent = 'Enter a discussion title.';
            return;
        }
        const controller = new AbortController();
        this.createController = controller;
        const timer = setTimeout(() => controller.abort(), 12000);
        button.disabled = true;
        this.createStatus.textContent = 'Creating thread…';
        try {
            const response = await fetch(this.root.dataset.createEndpoint, {
                method: 'POST', signal: controller.signal,
                headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-TOKEN': document.querySelector('meta[name="csrf-token"]').content },
                body: JSON.stringify({ title, body: String(fields.get('body') ?? ''), source_url: this.source, ...(this.root.dataset.searchReturn ? { from: this.root.dataset.searchReturn } : {}) }),
            });
            const payload = await response.json();
            if (sourceGeneration !== this.sourceGeneration) return;
            if (!response.ok) {
                this.root.querySelector('[data-thread-sign-in]').hidden = response.status !== 401;
                this.createStatus.textContent = response.status === 401 ? 'Your session expired. Sign in again to continue. Your draft is saved.'
                    : typeof payload.message === 'string' ? payload.message : 'Could not create the thread. Please try again.';
                return;
            }
            this.form.elements.title.value = '';
            this.form.elements.body.value = '';
            this.createStatus.textContent = 'Thread created.';
            if (Number.isSafeInteger(payload.data?.id)) {
                const link = textElement('a', 'mf-text-link mf-thread-open', 'Open your conversation →');
                link.href = this.link(`/threads/${payload.data.id}`);
                this.createStatus.append(link);
            }
            await this.load(1);
        } catch (_) {
            if (sourceGeneration === this.sourceGeneration) this.createStatus.textContent = 'Could not confirm whether your thread was created. Refresh the discussions before trying again.';
        } finally {
            clearTimeout(timer);
            if (sourceGeneration === this.sourceGeneration) button.disabled = false;
        }
    }
}
