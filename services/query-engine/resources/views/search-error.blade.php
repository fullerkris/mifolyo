<x-search-layout :title="$heading.' — MiFolyo'" :query="$query" :search-mode="$searchMode ?? 'web'">
    <main id="main" class="mf-page" tabindex="-1">
        <x-search-tabs :query="$query" :active="$searchMode ?? 'web'" />
        @if (($searchMode ?? 'web') === 'images')<x-search-return />@endif
        <div class="mf-empty-state mf-error-state" role="alert">
            <x-search-icon />
            <p class="mf-eyebrow">{{ ($searchMode ?? 'web') === 'images' ? 'Image search' : 'Search' }} · {{ $status }}</p>
            <h1>{{ $heading }}</h1>
            <p>{{ $message }}</p>
            @if ($retryUrl)
                <a href="{{ $retryUrl }}" class="mf-button mf-primary">Try again</a>
            @else
                <a href="{{ route('home') }}" class="mf-button mf-secondary">Back to search</a>
            @endif
        </div>
    </main>
</x-search-layout>
