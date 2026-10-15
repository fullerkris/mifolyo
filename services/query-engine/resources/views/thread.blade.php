<x-search-layout :title="$post['title'].' — MiFolyo'">
    @php($searchContext = \App\Support\SearchContext::parameters(request()))
    <main id="main" class="mf-page mf-discussion-page" tabindex="-1">
        @if ($sourceUrl || $searchContext)
            <nav class="mf-reading-nav" aria-label="Return navigation">
                <x-search-return />
                @if ($sourceUrl)
                    <a href="{{ route('discussions', ['url' => $sourceUrl] + $searchContext) }}" class="mf-text-link mf-back-link">← All discussions for this source</a>
                @endif
            </nav>
        @endif
        <article class="mf-thread mf-thread-detail">
            <p class="mf-eyebrow">{{ data_get($post, 'community.name', 'Community discussion') }}</p>
            <h1>{{ $post['title'] }}</h1>
            <p class="mf-thread-meta">{{ data_get($post, 'author.username') ?: 'Community member' }} · {{ $post['comment_count'] ?? 0 }} {{ ($post['comment_count'] ?? 0) == 1 ? 'reply' : 'replies' }}</p>
            @if ($sourceUrl)<p class="mf-thread-body"><a class="mf-source-link" href="{{ $sourceUrl }}" target="_blank" rel="noopener noreferrer">{{ $sourceUrl }} <x-search-icon name="external" /></a></p>@endif
            <p class="mf-thread-body">{{ $post['body'] ?? '' }}</p>
        </article>
        <section class="mf-comments" aria-labelledby="comments-heading">
            <h2 id="comments-heading">Comments &amp; replies</h2>
            @if ($commentsUnavailable)
                <p class="mf-form-error" role="alert">Replies are temporarily unavailable. <a href="{{ route('threads.show', ['thread' => $post['id']] + $searchContext) }}">Try again</a></p>
            @elseif (count($comments) === 0)
                <p class="mf-thread-status">No replies yet. Add the first perspective.</p>
            @else
                <ol class="mf-comment-list">
                    @foreach ($comments as $comment)
                        @include('components.thread-comment', ['comment' => $comment, 'thread' => $post, 'level' => 0])
                    @endforeach
                </ol>
            @endif
        </section>
        <section id="reply-form" class="mf-compose" tabindex="-1" aria-labelledby="reply-heading">
            <h2 id="reply-heading">{{ $reply ? 'Reply to '.(data_get($reply, 'author.username') ?: 'this comment') : 'Add your perspective' }}</h2>
            @error('body')<p class="mf-form-error" role="alert">{{ $message }}</p>@enderror
            @error('parent_comment_id')<p class="mf-form-error" role="alert">{{ $message }}</p>@enderror
            @if ($post['is_locked'] ?? false)
                <p>This conversation is closed to new replies.</p>
            @elseif ($replyUnavailable)
                <p>The selected comment is unavailable for replies. @if ($draft) Your draft is saved. @endif <a class="mf-text-link" href="{{ route('threads.show', ['thread' => $post['id'], 'reply_to' => 0] + $searchContext) }}#reply-form">Reply to the thread instead</a></p>
            @elseif (! session()->has('forum.user'))
                <p><a class="mf-text-link" href="{{ route('account.sign-in', ['return_to' => request()->getRequestUri()]) }}">Sign in to reply</a> or <a class="mf-text-link" href="{{ route('account.register', ['return_to' => request()->getRequestUri()]) }}">create an account</a> to join the conversation.</p>
            @else
                <form method="POST" action="{{ route('threads.comment', $post['id']) }}" class="mf-account-form" data-posting-form>
                    @csrf
                    @if ($searchContext)<input type="hidden" name="from" value="{{ $searchContext['from'] }}">@endif
                    <input type="hidden" name="parent_comment_id" value="{{ $reply['id'] ?? '' }}">
                    @if ($reply)<blockquote>{{ Str::limit($reply['body'], 200) }}</blockquote><a class="mf-text-link" href="{{ route('threads.show', ['thread' => $post['id'], 'reply_to' => 0] + $searchContext) }}#reply-form">Reply to the thread instead</a>@endif
                    <label for="reply-body">Your reply</label>
                    <textarea id="reply-body" name="body" rows="5" maxlength="10000" required>{{ old('body', $draft['body'] ?? '') }}</textarea>
                    <button type="submit" class="mf-button mf-primary">Post reply</button>
                </form>
            @endif
        </section>
    </main>
</x-search-layout>
