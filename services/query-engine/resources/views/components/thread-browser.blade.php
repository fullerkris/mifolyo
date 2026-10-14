@props(['prefix', 'sourceUrl' => ''])
<section class="mf-thread-browser" data-thread-browser data-source-url="{{ $sourceUrl }}" data-threads-endpoint="{{ route('community.threads', [], false) }}" data-create-endpoint="{{ route('community.create', [], false) }}">
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
    <p class="mf-sign-in-prompt" data-thread-sign-in @if (session()->has('forum.user')) hidden @endif>
        <a class="mf-text-link" data-forum-sign-in href="{{ route('account.sign-in', ['return_to' => '/discussions?'.http_build_query(['url' => $sourceUrl])]) }}">Sign in to start a thread</a>
        or <a class="mf-text-link" data-forum-register href="{{ route('account.register', ['return_to' => '/discussions?'.http_build_query(['url' => $sourceUrl])]) }}">create an account</a>.
    </p>
    @if (session()->has('forum.user'))
    @php($draft = old('source_url') === $sourceUrl ? old() : (session('forum_thread_draft.source_url') === $sourceUrl ? session('forum_thread_draft') : []))
    <details class="mf-create-thread" @if ($draft || $errors->any()) open @endif>
        <summary>Start a thread</summary>
        <form method="POST" action="{{ route('community.create') }}" data-thread-create>
            @csrf
            <input type="hidden" name="source_url" value="{{ $sourceUrl }}">
            <p>Attach your question or perspective to this source.</p>
            <label for="{{ $prefix }}-title">Discussion title</label>
            <input id="{{ $prefix }}-title" name="title" type="text" maxlength="300" placeholder="What should people discuss?" value="{{ $draft['title'] ?? '' }}" required>
            <label for="{{ $prefix }}-body">Body <span>optional</span></label>
            <textarea id="{{ $prefix }}-body" name="body" rows="4" maxlength="50000" placeholder="Add context, evidence, or a question.">{{ $draft['body'] ?? '' }}</textarea>
            <button type="submit" class="mf-button mf-primary">Start thread</button>
            <p data-create-status role="status" aria-live="polite"></p>
            @if ($errors->any())<div class="mf-form-error" role="alert">@foreach ($errors->all() as $error)<p>{{ $error }}</p>@endforeach</div>@endif
        </form>
    </details>
    @endif
</section>
