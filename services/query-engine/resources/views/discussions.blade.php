<x-search-layout :title="'Discussions for '.($sourceTitle ?: $sourceUrl).' — MiFolyo'">
    <main id="main" class="mf-page mf-discussion-page" tabindex="-1">
        <a href="{{ route('home') }}" class="mf-text-link mf-back-link">← Search MiFolyo</a>
        <header class="mf-discussion-heading">
            <p class="mf-eyebrow">Community discussion</p>
            <h1>{{ $sourceTitle ?: 'Discussions for this page' }}</h1>
            <a href="{{ $sourceUrl }}" class="mf-source-link" target="_blank" rel="noopener noreferrer">{{ $sourceUrl }} <x-search-icon name="external" /></a>
            <p>Different conversations. One shared source.</p>
        </header>
        <x-thread-browser prefix="page" :source-url="$sourceUrl" />
    </main>
</x-search-layout>
