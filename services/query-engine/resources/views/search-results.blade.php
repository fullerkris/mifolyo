<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    @vite('resources/css/app.css')
    <title>Search Results for "{{ $originalQuery }}"</title>
</head>

<body>
    <x-moogle-bar />
    <div class="results-counter">
        @php
            $suggestion = '';
            if ($suggestions) {
                $suggestion = "Did you mean $query? ";
            }
        @endphp
        <span id="suggestion">{{ $suggestion }}</span><span>Showing {{ $total }} results for
            {{ $query }}</span>
        @if ($suggestions)
            <p>Search for <a id="suggestion-link"
                    href="{{ route('search_force', ['processed_query' => $originalQuery]) }}">{{ $originalQuery }}</a>
                instead
            </p>
        @endif
    </div>

    <div class="results-container">
        <div class="pages-container col-span-2">
            @if (count($results) > 0)
                <ul>
                    @foreach ($results as $res)
                        <x-search-result url="{{ $res->_id }}" title="{{ $res->title }}"
                            text="{{ $res->summary_text }}" />
                    @endforeach
                </ul>
            @else
                <p>No results found</p>
            @endif
        </div>
        @if ($page != null && $page == 1)
            <div class="images-container flex flex-col items-center">
                <h2 class="top-images-text"> {{ ucwords($query) }} </h2>
                @foreach ($topImages as $res)
                    <div class="my-2">
                        <x-image-container url="{{ $res->_id }}" alt="{{ $res->alt }}"
                            title="{{ $res->page_title }}" page="{{ $res->page_url }}"
                            text="{{ $res->page_text }}" />
                    </div>
                @endforeach
            </div>
        @endif
        <div class="flex flex-col justify-center items-center">
            <x-pagination-bar totalResults="{{ $total }}" />
        </div>
    </div>

    <div id="thread-panel-backdrop" class="thread-panel-backdrop hidden" data-thread-panel-close></div>
    <aside id="thread-panel" class="thread-panel" aria-hidden="true" aria-labelledby="thread-panel-title">
        <div class="thread-panel-header">
            <div>
                <p class="thread-panel-kicker">Community discussion</p>
                <h2 id="thread-panel-title">Threads for this result</h2>
                <p id="thread-panel-source" class="thread-panel-source"></p>
            </div>
            <button type="button" class="thread-panel-close" data-thread-panel-close aria-label="Close discussion panel">
                ×
            </button>
        </div>
        <div id="thread-panel-status" class="thread-panel-status">Select a result to view discussions.</div>
        <div id="thread-panel-list" class="thread-panel-list"></div>
        <form id="thread-create-form" class="thread-create-form hidden">
            <h3>Start a thread</h3>
            <p class="thread-create-help">Create a discussion tied to this exact search result.</p>
            <label for="thread-create-token">Forum API token</label>
            <input id="thread-create-token" name="token" type="password" autocomplete="off"
                placeholder="Temporary beta token">
            <label for="thread-create-title">Title</label>
            <input id="thread-create-title" name="title" type="text" maxlength="300"
                placeholder="What should people discuss?" required>
            <label for="thread-create-body">Body <span>optional</span></label>
            <textarea id="thread-create-body" name="body" rows="4"
                placeholder="Add context, evidence, or a question."></textarea>
            <button type="submit" class="btn-discuss">Start thread</button>
            <p id="thread-create-message" class="thread-create-message"></p>
        </form>
    </aside>

    <!-- Footer -->
    <footer>
        <p> <a href="https://github.com/IonelPopJara/search-engine">Support the project!</a></p>
        <p>
            <a href="https://x.com/ionelalexandr12">Twitter</a> -
            <a href="https://www.youtube.com/multselmesco">YouTube</a> -
            <a href="https://github.com/IonelPopJara">GitHub</a>
        </p>
        <p id="copyright">©2025</p>
    </footer>
    <script>
        document.addEventListener('DOMContentLoaded', () => {
            const threadsEndpoint = '/api/threads/by-url';
            const createThreadEndpoint = '/api/threads';
            const panel = document.getElementById('thread-panel');
            const backdrop = document.getElementById('thread-panel-backdrop');
            const status = document.getElementById('thread-panel-status');
            const list = document.getElementById('thread-panel-list');
            const title = document.getElementById('thread-panel-title');
            const source = document.getElementById('thread-panel-source');
            const createForm = document.getElementById('thread-create-form');
            const createToken = document.getElementById('thread-create-token');
            const createTitle = document.getElementById('thread-create-title');
            const createBody = document.getElementById('thread-create-body');
            const createMessage = document.getElementById('thread-create-message');
            let activeRequestId = 0;
            let activeSourceUrl = '';
            let activeResultTitle = '';

            createToken.value = localStorage.getItem('mifolyo_api_token') || '';

            const openPanel = () => {
                panel.classList.add('open');
                panel.setAttribute('aria-hidden', 'false');
                backdrop.classList.remove('hidden');
            };

            const closePanel = () => {
                panel.classList.remove('open');
                panel.setAttribute('aria-hidden', 'true');
                backdrop.classList.add('hidden');
            };

            const showCreateForm = () => {
                createForm.classList.remove('hidden');
                createMessage.textContent = '';
            };

            const renderSource = (url) => {
                try {
                    const parsed = new URL(url);
                    return `${parsed.hostname}${parsed.pathname === '/' ? '' : parsed.pathname}`;
                } catch (error) {
                    return url;
                }
            };

            const renderThread = (thread) => {
                const author = thread.author?.username || 'unknown';
                const level = thread.author?.level || 1;
                const sourceLabel = `${thread.source_domain || ''}${thread.source_path || ''}`;

                return `
                    <article class="thread-panel-thread">
                        <h3>${escapeHtml(thread.title)}</h3>
                        <p class="thread-panel-thread-meta">${escapeHtml(author)} · Level ${level} · ${thread.comment_count} replies · ${thread.score} score</p>
                        <p class="thread-panel-thread-source">${escapeHtml(sourceLabel)}</p>
                    </article>
                `;
            };

            const escapeHtml = (value) => String(value ?? '')
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#039;');

            const loadThreads = async (url, resultTitle) => {
                const requestId = ++activeRequestId;
                activeSourceUrl = url;
                activeResultTitle = resultTitle || 'Threads for this result';
                title.textContent = resultTitle || 'Threads for this result';
                source.textContent = renderSource(url);
                list.innerHTML = '';
                createTitle.value = '';
                createBody.value = '';
                createMessage.textContent = '';
                status.textContent = 'Loading community threads...';
                openPanel();

                try {
                    const response = await fetch(`${threadsEndpoint}?url=${encodeURIComponent(url)}&sort=top`, {
                        headers: {
                            'Accept': 'application/json',
                        },
                    });

                    if (requestId !== activeRequestId) {
                        return;
                    }

                    if (!response.ok) {
                        status.textContent = response.status === 503
                            ? 'Forum threads are unavailable right now.'
                            : 'Could not load discussions for this result.';
                        return;
                    }

                    const payload = await response.json();
                    const threads = payload.data || [];

                    if (threads.length === 0) {
                        status.innerHTML = `No discussions yet for this page. <span>You can start the first thread below.</span>`;
                        showCreateForm();
                        return;
                    }

                    status.textContent = `${threads.length} discussion${threads.length === 1 ? '' : 's'} found`;
                    list.innerHTML = threads.map(renderThread).join('');
                    showCreateForm();
                } catch (error) {
                    if (requestId === activeRequestId) {
                        status.textContent = 'Could not reach the discussion service.';
                        createForm.classList.add('hidden');
                    }
                }
            };

            createForm.addEventListener('submit', async (event) => {
                event.preventDefault();

                if (!activeSourceUrl) {
                    createMessage.textContent = 'Choose a search result first.';
                    return;
                }

                const token = createToken.value.trim();
                const threadTitle = createTitle.value.trim();
                const body = createBody.value.trim();

                if (!threadTitle) {
                    createMessage.textContent = 'Add a title before starting a thread.';
                    return;
                }

                if (token) {
                    localStorage.setItem('mifolyo_api_token', token);
                }

                createMessage.textContent = 'Creating thread...';

                try {
                    const headers = {
                        'Accept': 'application/json',
                        'Content-Type': 'application/json',
                    };

                    if (token) {
                        headers.Authorization = `Bearer ${token}`;
                    }

                    const response = await fetch(createThreadEndpoint, {
                        method: 'POST',
                        headers,
                        body: JSON.stringify({
                            title: threadTitle,
                            body,
                            source_url: activeSourceUrl,
                        }),
                    });

                    const payload = await response.json().catch(() => ({}));

                    if (response.status === 401) {
                        createMessage.textContent = 'Log in to the forum, then paste your beta API token to start a thread.';
                        return;
                    }

                    if (response.status === 403) {
                        createMessage.textContent = payload.message || 'You do not have permission to post in this community.';
                        return;
                    }

                    if (!response.ok) {
                        createMessage.textContent = payload.message || 'Could not create the thread.';
                        return;
                    }

                    createTitle.value = '';
                    createBody.value = '';
                    createMessage.textContent = 'Thread created.';
                    await loadThreads(activeSourceUrl, activeResultTitle);
                } catch (error) {
                    createMessage.textContent = 'Could not reach the discussion service.';
                }
            });

            document.querySelectorAll('[data-discuss-url]').forEach((button) => {
                button.addEventListener('click', () => {
                    loadThreads(button.dataset.discussUrl, button.dataset.discussTitle);
                });
            });

            document.querySelectorAll('[data-thread-panel-close]').forEach((element) => {
                element.addEventListener('click', closePanel);
            });

            document.addEventListener('keydown', (event) => {
                if (event.key === 'Escape') {
                    closePanel();
                }
            });
        });
    </script>
</body>

</html>
