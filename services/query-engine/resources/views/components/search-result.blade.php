@php
    $source = is_string($url) ? $url : '';
    $validUrl = filter_var($source, FILTER_VALIDATE_URL) && in_array(strtolower(parse_url($source, PHP_URL_SCHEME) ?? ''), ['http', 'https'], true);
    $domain = $validUrl ? parse_url($source, PHP_URL_HOST) : '';
    $label = trim((string) $title) ?: ($domain ?: 'Untitled page');
    $searchReturn = \App\Support\SearchContext::url(request()->getRequestUri());
    $discussionPage = $validUrl ? route('discussions', array_filter(['url' => $source, 'title' => mb_substr($label, 0, 300), 'from' => $searchReturn], fn ($value) => $value !== null)) : null;
@endphp
<li class="mf-result-card">
    <article>
        <div class="mf-result-source"><span class="mf-source-monogram" aria-hidden="true">{{ mb_strtoupper(mb_substr($domain ?: '?', 0, 1)) }}</span><span>{{ $domain ?: 'Source URL unavailable' }}</span></div>
        <h2>@if ($validUrl)<a href="{{ $source }}">{{ $label }}</a>@else{{ $label }}@endif</h2>
        <p class="mf-result-excerpt">{{ Str::limit(trim((string) $text) ?: 'No excerpt is available. Open the source to read more.', 260) }}</p>
        @if ($validUrl)
            <div class="mf-result-actions">
                <a href="{{ $source }}" class="mf-button mf-primary">View page <x-search-icon name="external" /></a>
                <a href="{{ $discussionPage }}" class="mf-button mf-secondary" data-discuss-url="{{ $source }}" data-discuss-title="{{ $label }}"><x-search-icon name="chat" /> Discuss<span class="mf-discussion-count" data-discussion-count hidden></span><span class="mf-sr-only"> {{ $label }}</span></a>
                <a href="{{ route('page_connections', ['url' => $source]) }}" class="mf-text-link">Page links</a>
            </div>
        @endif
    </article>
</li>
