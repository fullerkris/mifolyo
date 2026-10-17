@props(['query' => '', 'hero' => false, 'mode' => 'web'])
<form class="mf-search-form {{ $hero ? 'mf-hero-search' : 'mf-header-search' }}" action="{{ $hero ? route('home.explore') : route($mode === 'images' ? 'search.images' : 'search') }}" method="GET" role="search" data-search-form data-mode="search">
    @if ($hero)
        <input type="hidden" name="mode" value="search">
    @endif
    <label for="{{ $hero ? 'home-query' : 'header-query' }}" class="mf-sr-only">{{ $mode === 'images' ? 'Search images' : 'Search the web' }}</label>
    <x-search-icon />
    <input id="{{ $hero ? 'home-query' : 'header-query' }}" name="q" type="search" value="{{ $query }}" placeholder="{{ $mode === 'images' ? 'Search indexed images...' : 'Search indexed pages...' }}" maxlength="500" autocomplete="off" enterkeyhint="search" required>
    <button type="submit" class="mf-search-submit" aria-label="Search"><x-search-icon /></button>
</form>
