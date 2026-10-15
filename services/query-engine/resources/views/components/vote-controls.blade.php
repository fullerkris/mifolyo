@props(['item', 'thread', 'type' => 'post'])
@php
    $id = (int) $item['id'];
    $current = in_array($item['viewer_vote'] ?? null, [-1, 0, 1], true) ? $item['viewer_vote'] : 0;
    $score = is_int($item['score'] ?? null) ? $item['score'] : null;
    $closed = ($thread['is_locked'] ?? false) || ($thread['is_removed'] ?? false) || ($item['is_locked'] ?? false) || ($item['is_removed'] ?? false);
    $removed = $item['is_removed'] ?? false;
    $signedIn = session()->has('forum.user');
    $back = \App\Support\ThreadContext::url(request(), (int) $thread['id'], $type === 'comment' ? $id : null);
    $context = \App\Support\SearchContext::parameters(request());
    $replyTo = filter_var(request('reply_to'), FILTER_VALIDATE_INT, ['options' => ['min_range' => 0]]);
    $feedback = session('vote_feedback', []);
    $feedback = ($feedback['type'] ?? null) === $type && ($feedback['id'] ?? null) === $id ? $feedback : [];
    $label = $type === 'post' ? 'thread' : 'comment by '.(data_get($item, 'author.username') ?: 'Community member');
@endphp
<div @if ($type === 'post') id="thread-votes" @endif class="mf-vote" role="group" aria-label="Votes for {{ $label }}" data-vote data-vote-type="{{ $type }}" data-vote-id="{{ $id }}" data-current-vote="{{ $current }}">
    @if ($removed)
        <p class="mf-vote-status">Voting unavailable for removed content.</p>
    @else
        <form method="POST" action="{{ route('threads.vote', $thread['id']) }}" data-vote-form>
            @csrf
            <input type="hidden" name="votable_type" value="{{ $type }}">
            <input type="hidden" name="votable_id" value="{{ $id }}">
            @if ($context)<input type="hidden" name="from" value="{{ $context['from'] }}">@endif
            @if ($replyTo !== false && $replyTo !== null)<input type="hidden" name="reply_to" value="{{ $replyTo }}">@endif
            <div class="mf-vote-controls">
                <button type="submit" name="value" value="{{ $current === 1 ? 0 : 1 }}" class="mf-vote-button" data-vote-direction="1" aria-label="Upvote {{ $label }}" aria-pressed="{{ $current === 1 ? 'true' : 'false' }}" @disabled($closed || ! $signedIn || $score === null)><x-search-icon name="up" /></button>
                <span class="mf-vote-score"><span data-vote-score>{{ $score === null ? '—' : number_format($score) }}</span><span class="mf-sr-only"> score</span></span>
                <button type="submit" name="value" value="{{ $current === -1 ? 0 : -1 }}" class="mf-vote-button" data-vote-direction="-1" aria-label="Downvote {{ $label }}" aria-pressed="{{ $current === -1 ? 'true' : 'false' }}" @disabled($closed || ! $signedIn || $score === null)><x-search-icon name="down" /></button>
                @if ($closed)<span class="mf-vote-hint">Voting closed</span>@elseif ($score === null)<span class="mf-vote-hint">Score unavailable</span>@endif
            </div>
            <button type="submit" name="value" value="{{ $feedback['retry_value'] ?? '' }}" class="mf-button mf-secondary" data-vote-retry @if (! array_key_exists('retry_value', $feedback) || $closed || ! $signedIn) hidden @endif>Retry vote</button>
        </form>
        <p class="mf-vote-status {{ ! empty($feedback['error']) ? 'mf-vote-error' : '' }}" data-vote-status role="status" aria-atomic="true">{{ $feedback['message'] ?? '' }}</p>
        <a class="mf-text-link mf-vote-sign-in" data-vote-sign-in href="{{ route('account.sign-in', ['return_to' => $back]) }}" @if ($signedIn || $closed) hidden @endif>Sign in to vote</a>
        <a class="mf-text-link mf-vote-reload" data-vote-reload href="{{ $back }}" hidden>Reload conversation</a>
    @endif
</div>
