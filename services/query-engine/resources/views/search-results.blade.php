@php
    $displayQuery = $suggestions ? $query : ($originalQuery ?? $query);
@endphp
<x-search-layout :title="'Search results for '.($originalQuery ?? $query).' — MiFolyo'" :query="$originalQuery ?? $query">
    <main id="main" class="mf-page" tabindex="-1" data-search-context="{{ \App\Support\SearchContext::url(request()->getRequestUri()) }}" data-counts-endpoint="{{ route('community.counts', [], false) }}">
        <x-search-tabs :query="$originalQuery ?? $query" />
        <div class="mf-results-heading">
            <div><p class="mf-eyebrow">Web search</p><h1>{{ $displayQuery !== '' ? 'Results for “'.$displayQuery.'”' : 'What are you curious about?' }}</h1></div>
            @if ($total > 0)
                <p class="mf-muted">{{ number_format($total) }} {{ $total == 1 ? 'result' : 'results' }} · Page {{ $page }}</p>
            @endif
        </div>
        @if ($suggestions)
            <p class="mf-spelling">Showing results for <strong>{{ $query }}</strong>. Search for <a href="{{ route('search.exact', ['q' => $originalQuery]) }}">{{ $originalQuery }}</a> instead.</p>
        @endif
        <div class="mf-results-grid">
            <section aria-label="Search results" class="mf-results">
                <p class="mf-counts-status" data-counts-status role="status" hidden>Some conversation counts are unavailable. <button type="button" class="mf-text-link" data-counts-retry>Retry counts</button></p>
                @if (count($results) > 0)
                    @php
                        $presenter = new \App\Support\SearchResultPresenter($displayQuery);
                        $discussHintIndex = \App\Services\Onboarding::state(request())['discuss']
                            ? collect($results)->search(fn ($res) => $presenter::isSourceUrl(data_get($res, '_id'))) : false;
                    @endphp
                    <ul class="mf-results-list">
                        @foreach ($results as $index => $res)
                            <x-search-result :url="data_get($res, '_id')" :title="data_get($res, 'title')" :summary="data_get($res, 'summary_text')" :description="data_get($res, 'description')" :presenter="$presenter" :onboarding-hint="$index === $discussHintIndex" />
                        @endforeach
                    </ul>
                @else
                    <div class="mf-empty-state">
                        <x-search-icon />
                        <h2>{{ $query === '' ? 'Start with a search' : ($total > 0 ? 'No results on this page' : 'No results found') }}</h2>
                        <p>{{ $total > 0 ? 'Go back to the first page to explore the available results.' : 'Try a different phrase or fewer words. You can edit your search above.' }}</p>
                        @if ($total > 0)
                            <a class="mf-button mf-primary" href="{{ request()->url().'?'.http_build_query(['q' => $originalQuery ?? $query, 'page' => 1], '', '&', PHP_QUERY_RFC3986) }}">Back to first page</a>
                        @endif
                    </div>
                @endif
                <x-pagination-bar :total-results="$total" />
            </section>
            <aside class="mf-search-aside" aria-label="About these results">
                <div class="mf-context-card">
                    <x-search-icon name="chat" />
                    <p class="mf-eyebrow">Beyond the link</p>
                    <h2>There’s more to a result.</h2>
                    <p>Questions, experiences, and useful context belong alongside the source.</p>
                    <p>Choose <strong>Discuss</strong> on any result to explore its conversations.</p>
                </div>
                <p class="mf-aside-note">Results come from MiFolyo’s indexed pages. Open the original source for its latest content.</p>
                @if (count($topImages) > 0)
                    <section class="mf-related-images" aria-label="Related images">
                        <h2>Related images</h2>
                        <a class="mf-text-link" href="{{ route('search.images', ['q' => $originalQuery ?? $query, 'from' => \App\Support\SearchContext::url(request()->getRequestUri())]) }}">Explore all images →</a>
                        @foreach ($topImages as $image)
                            @php
                                $imageUrl = data_get($image, '_id', '');
                                $imagePage = data_get($image, 'page_url', '');
                                $isHttp = fn ($value) => is_string($value) && filter_var($value, FILTER_VALIDATE_URL) && in_array(strtolower(parse_url($value, PHP_URL_SCHEME) ?? ''), ['http', 'https'], true);
                            @endphp
                            @if ($isHttp($imageUrl) && $isHttp($imagePage))
                                <a href="{{ $imagePage }}"><span class="mf-related-preview"><span class="mf-image-fallback" aria-hidden="true">Preview unavailable</span><img src="{{ $imageUrl }}" alt="{{ \App\Support\SearchResultPresenter::text(data_get($image, 'alt'), 500) }}" loading="lazy" referrerpolicy="no-referrer" data-image-preview></span><span>{{ \App\Support\SearchResultPresenter::text(data_get($image, 'page_title'), 300) ?: 'View image source' }}</span></a>
                            @endif
                        @endforeach
                    </section>
                @endif
            </aside>
        </div>
    </main>
    <dialog class="mf-discussion-dialog" data-discussion-dialog aria-labelledby="panel-title">
        <div class="mf-dialog-heading">
            <div><p class="mf-eyebrow">Community discussion</p><h2 id="panel-title" data-panel-title>Discussions for this page</h2></div>
            <button type="button" class="mf-icon-button" data-close-discussions aria-label="Close discussions" autofocus><x-search-icon name="close" /></button>
        </div>
        <a class="mf-source-link" data-panel-source target="_blank" rel="noopener noreferrer"></a>
        <a class="mf-full-page-link" data-panel-page>Open discussion page →</a>
        <x-thread-browser prefix="panel" />
    </dialog>
</x-search-layout>
