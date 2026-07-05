<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    @vite('resources/css/app.css')
    <title>Discussions for {{ $sourceTitle ?: $sourceUrl }}</title>
</head>

<body>
    <main class="discussion-page">
        <a href="/" class="discussion-page-logo">Moogle!</a>

        <section class="discussion-page-hero">
            <p class="thread-panel-kicker">Community discussion</p>
            <h1>{{ $sourceTitle ?: 'Threads for this result' }}</h1>
            <a href="{{ $sourceUrl }}" class="discussion-page-source" target="_blank" rel="noopener noreferrer">
                {{ $sourceUrl }}
            </a>
        </section>

        <section class="discussion-page-grid">
            <div>
                <div id="discussion-page-status" class="thread-panel-status">Loading community threads...</div>
                <div id="discussion-page-list" class="thread-panel-list"></div>
            </div>

            <form id="discussion-create-form" class="thread-create-form">
                <h2>Start a thread</h2>
                <p class="thread-create-help">Create a discussion tied to this exact search result.</p>
                <label for="discussion-create-token">Forum API token</label>
                <input id="discussion-create-token" name="token" type="password" autocomplete="off"
                    placeholder="Temporary beta token">
                <label for="discussion-create-title">Title</label>
                <input id="discussion-create-title" name="title" type="text" maxlength="300"
                    placeholder="What should people discuss?" required>
                <label for="discussion-create-body">Body <span>optional</span></label>
                <textarea id="discussion-create-body" name="body" rows="6"
                    placeholder="Add context, evidence, or a question."></textarea>
                <button type="submit" class="btn-discuss">Start thread</button>
                <p id="discussion-create-message" class="thread-create-message"></p>
            </form>
        </section>
    </main>

    <script>
        document.addEventListener('DOMContentLoaded', () => {
            const sourceUrl = @json($sourceUrl);
            const threadsEndpoint = '/api/threads/by-url';
            const createThreadEndpoint = '/api/threads';
            const status = document.getElementById('discussion-page-status');
            const list = document.getElementById('discussion-page-list');
            const createForm = document.getElementById('discussion-create-form');
            const createToken = document.getElementById('discussion-create-token');
            const createTitle = document.getElementById('discussion-create-title');
            const createBody = document.getElementById('discussion-create-body');
            const createMessage = document.getElementById('discussion-create-message');

            createToken.value = localStorage.getItem('mifolyo_api_token') || '';

            const escapeHtml = (value) => String(value ?? '')
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#039;');

            const renderThread = (thread) => {
                const author = thread.author?.username || 'unknown';
                const level = thread.author?.level || 1;
                const sourceLabel = `${thread.source_domain || ''}${thread.source_path || ''}`;
                const body = thread.body ? `<p class="discussion-page-thread-body">${escapeHtml(thread.body)}</p>` : '';

                return `
                    <article class="thread-panel-thread discussion-page-thread">
                        <h2>${escapeHtml(thread.title)}</h2>
                        <p class="thread-panel-thread-meta">${escapeHtml(author)} · Level ${level} · ${thread.comment_count} replies · ${thread.score} score</p>
                        ${body}
                        <p class="thread-panel-thread-source">${escapeHtml(sourceLabel)}</p>
                    </article>
                `;
            };

            const loadThreads = async () => {
                list.innerHTML = '';
                status.textContent = 'Loading community threads...';

                try {
                    const response = await fetch(`${threadsEndpoint}?url=${encodeURIComponent(sourceUrl)}&sort=top`, {
                        headers: {
                            'Accept': 'application/json',
                        },
                    });

                    if (!response.ok) {
                        status.textContent = response.status === 503
                            ? 'Forum threads are unavailable right now.'
                            : 'Could not load discussions for this result.';
                        return;
                    }

                    const payload = await response.json();
                    const threads = payload.data || [];

                    if (threads.length === 0) {
                        status.innerHTML = 'No discussions yet for this page. <span>Start the first thread.</span>';
                        return;
                    }

                    status.textContent = `${threads.length} discussion${threads.length === 1 ? '' : 's'} found`;
                    list.innerHTML = threads.map(renderThread).join('');
                } catch (error) {
                    status.textContent = 'Could not reach the discussion service.';
                }
            };

            createForm.addEventListener('submit', async (event) => {
                event.preventDefault();

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
                            source_url: sourceUrl,
                        }),
                    });

                    const payload = await response.json().catch(() => ({}));

                    if (response.status === 401) {
                        createMessage.textContent = 'Log in to the forum, then paste your beta API token to start a thread.';
                        return;
                    }

                    if (!response.ok) {
                        createMessage.textContent = payload.message || 'Could not create the thread.';
                        return;
                    }

                    createTitle.value = '';
                    createBody.value = '';
                    createMessage.textContent = 'Thread created.';
                    await loadThreads();
                } catch (error) {
                    createMessage.textContent = 'Could not reach the discussion service.';
                }
            });

            loadThreads();
        });
    </script>
</body>

</html>
