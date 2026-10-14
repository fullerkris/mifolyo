@props(['title' => 'MiFolyo', 'query' => '', 'home' => false])
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="color-scheme" content="light dark">
    <title>{{ $title }}</title>
    <link rel="icon" href="/mifolyo-mark.svg" type="image/svg+xml">
    <script>
        (() => {
            let theme;
            try { theme = localStorage.getItem('mifolyo-theme'); } catch (_) {}
            document.documentElement.dataset.theme = ['light', 'dark'].includes(theme)
                ? theme : (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
        })();
    </script>
    @vite(['resources/css/search-ui.css', 'resources/js/search-ui.js'])
</head>
<body class="mf-app">
    <a href="#main" class="mf-skip-link">Skip to content</a>
    <header class="mf-header {{ $home ? 'mf-home-header' : '' }}">
        <a href="{{ route('home') }}" class="mf-brand" aria-label="MiFolyo home">
            <img src="/mifolyo-mark.svg" width="100" height="32" alt=""><span>MiFolyo</span>
        </a>
        @unless ($home)
            <x-search-form :query="$query" />
        @endunless
        <nav class="mf-header-actions" aria-label="Main navigation">
            <a href="{{ route('home') }}#how-it-works">How it works</a>
            <button type="button" class="mf-icon-button" data-theme-toggle aria-label="Switch color theme" aria-pressed="false" hidden><x-search-icon name="theme" /></button>
        </nav>
    </header>
    <div class="mf-navigation-status" role="status" data-navigation-status hidden>Searching indexed pages…</div>
    {{ $slot }}
    <footer class="mf-footer">
        <span>Made for curious people.</span>
        <a href="{{ route('home') }}#about">About MiFolyo</a>
        <span>Search-first. Community-powered.</span>
    </footer>
</body>
</html>
