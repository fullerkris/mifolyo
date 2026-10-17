@php
    $source = \App\Support\SourceUrl::http($page_url, true);
    $image = \App\Support\SourceUrl::http($url, true);
    $imageAlt = \App\Support\SearchResultPresenter::text($alt, 500);
    $imageTitle = \App\Support\SearchResultPresenter::text($title, 2048);
    $label = $imageTitle !== '' ? $imageTitle : ($imageAlt !== '' ? $imageAlt : 'Untitled image');
    $shortTitle = \App\Support\SearchResultPresenter::shorten($label, 110);
    $excerpt = \App\Support\SearchResultPresenter::shorten(\App\Support\SearchResultPresenter::text($text), 160);
    $from = \App\Support\SearchContext::url(request()->getRequestUri());
@endphp
<article class="mf-image-card">
    <div class="mf-image-preview">
        <span class="mf-image-fallback" aria-hidden="{{ $image ? 'true' : 'false' }}">Preview unavailable</span>
        @if ($image)<img src="{{ $image }}" alt="{{ $imageAlt !== '' ? $imageAlt : $shortTitle }}" loading="lazy" decoding="async" referrerpolicy="no-referrer" data-image-preview>@endif
    </div>
    <div class="mf-image-content">
        <p class="mf-result-source"><bdi dir="ltr">{{ $source ? parse_url($source, PHP_URL_HOST) : 'Source page unavailable' }}</bdi></p>
        <h2>@if ($source)<a href="{{ $source }}">{{ $shortTitle }}</a>@else{{ $shortTitle }}@endif</h2>
        @if ($shortTitle !== $label)<details class="mf-result-details"><summary>Full title</summary><p>{{ $label }}</p></details>@endif
        @if ($excerpt !== '')<p class="mf-result-excerpt">{{ $excerpt }}</p>@endif
        <div class="mf-image-actions">
            @if ($source)<a class="mf-button mf-primary" href="{{ $source }}">View source <x-search-icon name="external" /></a>@endif
            @if ($image)<a class="mf-button mf-secondary" href="{{ $image }}">Open image</a>@endif
        </div>
        @if ($source)
            <div class="mf-image-context">
                <a class="mf-text-link" href="{{ route('discussions', array_filter(['url' => $source, 'title' => mb_substr($label, 0, 300), 'from' => $from])) }}">Discuss source</a>
                <a class="mf-text-link" href="{{ route('page.links', array_filter(['url' => $source, 'from' => $from])) }}">Page links</a>
            </div>
        @endif
    </div>
</article>
