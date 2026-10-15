@props(['query' => '', 'active' => 'web'])
@php
    $imageParams = ['q' => $query];
    $from = \App\Support\SearchContext::url(request()->getRequestUri());
    if ($active === 'web' && $from) $imageParams['from'] = $from;
    $webReturn = $active === 'images' ? \App\Support\SearchContext::url(request()->query('from'), false) : null;
@endphp
<nav class="mf-search-tabs" aria-label="Search type">
    <a href="{{ $webReturn ? $webReturn.'#mf-return' : route('search', ['q' => $query]) }}" @if ($active === 'web') aria-current="page" @endif><x-search-icon /> Web</a>
    <a href="{{ route('search.images', $imageParams) }}" @if ($active === 'images') aria-current="page" @endif><x-search-icon name="image" /> Images</a>
</nav>
