@php
    $label = $title !== '' ? $title : (parse_url($url, PHP_URL_HOST) ?: 'Unindexed page');
    $from = \App\Support\SearchContext::parameters(request());
@endphp
<x-search-layout title="Page links — MiFolyo">
    <main id="main" class="mf-page" tabindex="-1">
        <x-search-return />
        <div class="mf-results-heading">
            <div><p class="mf-eyebrow">Follow your curiosity</p><h1>Page links</h1></div>
            <p class="mf-muted">Discover how indexed pages connect.</p>
        </div>
        <x-page-url-form :url="$url" />
        @if ($url === '')
            <div class="mf-empty-state"><x-search-icon name="links" /><h2>Every page is part of a bigger picture.</h2><p>Enter a page URL to explore its outgoing links and the indexed pages that link to it.</p></div>
        @else
            <section class="mf-page-source" aria-label="Selected source">
                <div><p class="mf-eyebrow">Selected source</p><h2>{{ \App\Support\SearchResultPresenter::shorten($label, 140) }}</h2><a class="mf-source-link" href="{{ $url }}"><bdi dir="ltr">{{ $url }}</bdi></a>
                    @if (mb_strlen($label) > 140)<details class="mf-result-details"><summary>Full title</summary><p>{{ $label }}</p></details>@endif
                </div>
                <div class="mf-image-actions"><a class="mf-button mf-primary" href="{{ $url }}">View page <x-search-icon name="external" /></a><a class="mf-button mf-secondary" href="{{ route('discussions', ['url' => $url, 'title' => mb_substr($label, 0, 300)] + $from) }}">Discuss source</a></div>
            </section>
            <nav class="mf-link-directions" aria-label="Link direction">
                @foreach (['outgoing' => ['Outgoing links', 'Pages this source links to'], 'incoming' => ['Incoming links', 'Indexed pages linking here']] as $key => [$heading, $description])
                    <a href="{{ route('page.links', ['url' => $url, 'direction' => $key] + $from) }}" @if ($direction === $key) aria-current="page" @endif><span>{{ $heading }} <strong>{{ number_format($counts[$key]) }}</strong></span><small>{{ $description }}</small></a>
                @endforeach
            </nav>
            @if (count($mapLinks) > 0)
                <x-connection-map :url="$url" :label="$label" :links="$mapLinks" :direction="$direction" />
            @endif
            <div class="mf-connections-grid">
                <section aria-label="{{ ucfirst($direction) }} links">
                    <div class="mf-connections-heading"><h2>{{ ucfirst($direction) }} links</h2>@if ($counts[$direction] > 0)<span class="mf-muted">Page {{ $page }}</span>@endif</div>
                    @if (count($connections) > 0)
                        <ul class="mf-connection-list">
                            @foreach ($connections as $link)
                                @php
                                    $linkLabel = $link['title'] !== '' ? $link['title'] : (parse_url($link['url'], PHP_URL_HOST) ?: 'Untitled page');
                                @endphp
                                <li>
                                    <h3><a href="{{ $link['url'] }}">{{ \App\Support\SearchResultPresenter::shorten($linkLabel, 140) }}</a></h3>
                                    @if (mb_strlen($linkLabel) > 140)<details class="mf-result-details"><summary>Full title</summary><p>{{ $linkLabel }}</p></details>@endif
                                    <p class="mf-result-path"><bdi dir="ltr">{{ $link['url'] }}</bdi></p>
                                    <div class="mf-connection-actions">
                                        <span class="mf-muted">{{ $link['incoming'] && $link['outgoing'] ? 'Links both ways' : ($link['title'] === '' ? 'Page title not indexed' : ($direction === 'outgoing' ? 'Linked from this source' : 'Links to this source')) }}</span>
                                        <a class="mf-text-link" href="{{ route('page.links', ['url' => $link['url']] + $from) }}">Explore links <span class="mf-sr-only">for {{ $linkLabel }}</span> →</a>
                                    </div>
                                </li>
                            @endforeach
                        </ul>
                    @else
                        <div class="mf-empty-state"><x-search-icon name="links" /><h2>{{ $counts[$direction] > 0 ? 'No links on this page' : 'No '.$direction.' links indexed' }}</h2><p>{{ $counts[$direction] > 0 ? 'Return to the first page to explore the available connections.' : 'The index may not yet include these connections. This does not mean the page has no links on the web.' }}</p>
                            @if ($counts[$direction] > 0)<a class="mf-button mf-primary" href="{{ route('page.links', $parameters) }}">Back to first page</a>@endif
                        </div>
                    @endif
                    <x-pagination-bar :total-results="$counts[$direction]" :parameters="$parameters" label="Page link result pages" />
                </section>
                <aside class="mf-connection-aside">
                    <div class="mf-context-card"><x-search-icon name="links" /><p class="mf-eyebrow">A little more context</p><h2>Follow the connections.</h2><p>Outgoing links lead to the sources a page references. Incoming links show where other indexed pages point to it.</p><p>Connections are an index snapshot, not a live crawl or an endorsement.</p></div>
                </aside>
            </div>
        @endif
    </main>
</x-search-layout>
