@php
    $displayQuery = $originalQuery ?? $query;
    $parameters = ['q' => $displayQuery] + \App\Support\SearchContext::parameters(request());
@endphp
<x-search-layout :title="'Images for '.$displayQuery.' — MiFolyo'" :query="$displayQuery" search-mode="images">
    <main id="main" class="mf-page" tabindex="-1" data-search-context="{{ \App\Support\SearchContext::url(request()->getRequestUri()) }}">
        <x-search-tabs :query="$displayQuery" active="images" />
        <x-search-return />
        <div class="mf-results-heading">
            <div><p class="mf-eyebrow">Image search</p><h1>{{ trim($displayQuery) !== '' ? 'Images for “'.$displayQuery.'”' : 'Find a different perspective.' }}</h1></div>
            @if ($total > 0)<p class="mf-muted">{{ number_format($total) }} {{ $total == 1 ? 'image' : 'images' }} · Page {{ $page }}</p>@endif
        </div>
        @if (count($results) > 0)
            <p class="mf-discovery-note">Images from indexed pages. Visit the source for context and image usage information.</p>
            <ul class="mf-image-grid" aria-label="Image results">
                @foreach ($results as $res)
                    <li><x-image-container :url="data_get($res, '_id')" :alt="data_get($res, 'alt')" :title="data_get($res, 'page_title')" :page="data_get($res, 'page_url')" :text="data_get($res, 'page_text')" /></li>
                @endforeach
            </ul>
        @else
            <div class="mf-empty-state">
                <x-search-icon name="image" />
                <h2>{{ trim($displayQuery) === '' ? 'Start with an image search' : ($total > 0 ? 'No images on this page' : 'No images found') }}</h2>
                <p>{{ $total > 0 ? 'Return to the first page to browse the available images.' : 'Try a different phrase or fewer words. You can switch to Web to explore indexed pages.' }}</p>
                @if ($total > 0)<a class="mf-button mf-primary" href="{{ route('search.images', $parameters) }}">Back to first page</a>@endif
            </div>
        @endif
        <x-pagination-bar :total-results="$total" :parameters="$parameters" label="Image result pages" />
    </main>
</x-search-layout>
