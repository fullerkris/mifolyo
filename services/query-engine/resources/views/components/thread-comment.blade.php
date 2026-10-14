<li id="comment-{{ $comment['id'] }}" class="mf-comment">
    <article>
        <p class="mf-thread-meta">{{ data_get($comment, 'author.username') ?: 'Community member' }}{{ $level > 0 ? ' · Reply' : '' }}</p>
        <p class="mf-comment-body">{{ ($comment['is_removed'] ?? false) ? '[Comment removed]' : ($comment['body'] ?? '') }}</p>
        @unless (($thread['is_locked'] ?? false) || ($comment['is_locked'] ?? false) || ($comment['is_removed'] ?? false) || ($comment['depth'] ?? 0) >= 8)
            <a class="mf-text-link" href="{{ route('threads.show', ['thread' => $thread['id'], 'reply_to' => $comment['id']]) }}#reply-form">Reply<span class="mf-sr-only"> to {{ data_get($comment, 'author.username') ?: 'this comment' }}</span></a>
        @endunless
    </article>
    @if (! empty($comment['children']) && $level < 8)
        <ol class="mf-comment-list mf-comment-children">
            @foreach ($comment['children'] as $child)
                @include('components.thread-comment', ['comment' => $child, 'thread' => $thread, 'level' => $level + 1])
            @endforeach
        </ol>
    @endif
</li>
