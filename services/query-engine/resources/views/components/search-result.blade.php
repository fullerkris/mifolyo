@php
    $source = is_string($url) ? $url : '';
    $validUrl = strlen($source) <= 2048 && filter_var($source, FILTER_VALIDATE_URL) && in_array(strtolower(parse_url($source, PHP_URL_SCHEME) ?? ''), ['http', 'https'], true);
    $parts = $validUrl ? parse_url($source) : [];
    $domain = ($parts['host'] ?? '').(isset($parts['port']) ? ':'.$parts['port'] : '');
    $path = ($parts['path'] ?? '/').(isset($parts['query']) ? '?'.$parts['query'] : '').(isset($parts['fragment']) ? '#'.$parts['fragment'] : '');
    $shortPath = mb_strlen($path) > 100 ? mb_substr($path, 0, 72).'…'.mb_substr($path, -24) : $path;
    $indexedTitle = \App\Support\SearchResultPresenter::text($title, 2048);
    $label = $indexedTitle !== '' ? $indexedTitle : ($domain ?: 'Untitled page');
    $shortTitle = \App\Support\SearchResultPresenter::shorten($label, 140);
    $excerpt = $presenter->excerpt($summary, $description);
    $longTitle = $shortTitle !== $label;
    $longUrl = $validUrl && $shortPath !== $path;
    $searchReturn = \App\Support\SearchContext::url(request()->getRequestUri());
    $discussionPage = $validUrl ? route('discussions', array_filter(['url' => $source, 'title' => mb_substr($label, 0, 300), 'from' => $searchReturn], fn ($value) => $value !== null)) : null;
@endphp
<li class="mf-result-card">
    <article>
        <div class="mf-result-source">
            <span class="mf-source-monogram" aria-hidden="true">{{ mb_strtoupper(mb_substr($domain ?: '?', 0, 1)) }}</span>
            <div class="mf-result-location"><bdi class="mf-result-domain" dir="ltr">{{ $domain ?: 'Source URL unavailable' }}</bdi>@if ($validUrl && $path !== '/')<bdi class="mf-result-path" dir="ltr" title="{{ $source }}">{{ $shortPath }}</bdi>@endif</div>
        </div>
        <h2>@if ($validUrl)<a href="{{ $source }}" @if ($longTitle) title="{{ $label }}" @endif><x-result-text :parts="$presenter->parts($shortTitle)" /></a>@else<x-result-text :parts="$presenter->parts($shortTitle)" />@endif</h2>
        @if ($longTitle || $longUrl)
            <details class="mf-result-details">
                <summary>{{ $longTitle && $longUrl ? 'Full title and URL' : ($longTitle ? 'Full title' : 'Full URL') }}</summary>
                @if ($longTitle)<p class="mf-result-full-title"><x-result-text :parts="$presenter->parts($label)" /></p>@endif
                @if ($longUrl)<a href="{{ $source }}"><bdi dir="ltr">{{ $source }}</bdi></a>@endif
            </details>
        @endif
        <p class="mf-result-excerpt {{ $excerpt === '' ? 'mf-result-excerpt-empty' : '' }}">@if ($excerpt !== '')<x-result-text :parts="$presenter->parts($excerpt)" />@else{{ $validUrl ? 'No excerpt is available. Open the source to read more.' : 'No excerpt is available for this indexed entry.' }}@endif</p>
        @if ($validUrl)
            <div class="mf-result-actions">
                <a href="{{ $source }}" class="mf-button mf-primary">View page <x-search-icon name="external" /></a>
                <a href="{{ $discussionPage }}" class="mf-button mf-secondary" data-discuss-url="{{ $source }}" data-discuss-title="{{ $label }}"><x-search-icon name="chat" /> Discuss<span class="mf-discussion-count" data-discussion-count hidden></span><span class="mf-sr-only"> {{ $label }}</span></a>
                <a href="{{ route('page_connections', ['url' => $source]) }}" class="mf-text-link">Page links</a>
            </div>
        @endif
    </article>
</li>
