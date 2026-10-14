@props(['prefix', 'sourceUrl' => ''])
<section class="mf-thread-browser" data-thread-browser data-source-url="{{ $sourceUrl }}" data-threads-endpoint="/api/threads/by-url" data-create-endpoint="/api/threads">
    <div class="mf-thread-toolbar">
        <h2>Conversations</h2>
        <label for="{{ $prefix }}-sort" data-thread-sort-label hidden>Sort <select id="{{ $prefix }}-sort" data-thread-sort><option value="top">Top</option><option value="new">Newest</option></select></label>
    </div>
    <div class="mf-thread-status" role="status" aria-live="polite" data-thread-status>Enable JavaScript to load community discussions.</div>
    <button type="button" class="mf-button mf-secondary" data-thread-retry hidden>Try again</button>
    <div class="mf-thread-list" data-thread-list></div>
    <nav class="mf-thread-pagination" aria-label="Discussion pages" data-thread-pagination hidden>
        <button type="button" class="mf-button mf-secondary" data-thread-previous>Previous</button>
        <span data-thread-page></span>
        <button type="button" class="mf-button mf-secondary" data-thread-next>Next</button>
    </nav>
    <details class="mf-create-thread" hidden>
        <summary>Start a thread <span>Beta access</span></summary>
        <form method="POST" action="/api/threads" data-thread-create>
            <p>Attach your question or perspective to this source. Posting currently requires a forum API token.</p>
            <label for="{{ $prefix }}-token">Forum API token</label>
            <input id="{{ $prefix }}-token" name="token" type="password" autocomplete="off" required>
            <label for="{{ $prefix }}-title">Discussion title</label>
            <input id="{{ $prefix }}-title" name="title" type="text" maxlength="300" placeholder="What should people discuss?" required>
            <label for="{{ $prefix }}-body">Body <span>optional</span></label>
            <textarea id="{{ $prefix }}-body" name="body" rows="4" maxlength="50000" placeholder="Add context, evidence, or a question."></textarea>
            <button type="submit" class="mf-button mf-primary">Start thread</button>
            <p data-create-status role="status" aria-live="polite"></p>
        </form>
    </details>
</section>
