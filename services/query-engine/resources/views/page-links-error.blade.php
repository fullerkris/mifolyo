<x-search-layout :title="$heading.' — MiFolyo'">
    <main id="main" class="mf-page" tabindex="-1">
        <x-search-return />
        <x-page-url-form :url="$url" />
        <div class="mf-empty-state mf-error-state" role="alert">
            <x-search-icon name="links" />
            <p class="mf-eyebrow">Page links · {{ $status }}</p>
            <h1>{{ $heading }}</h1>
            <p>{{ $message }}</p>
            @if ($retryUrl)<a href="{{ $retryUrl }}" class="mf-button mf-primary">Try again</a>@endif
        </div>
    </main>
</x-search-layout>
