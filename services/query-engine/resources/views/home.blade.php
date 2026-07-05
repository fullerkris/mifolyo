<!DOCTYPE html>
<html lang="en">

<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    @vite('resources/css/app.css')
    <title>MiFolyo</title>
</head>

<body class="mifolyo-home-body">
    <main class="mifolyo-home">
        <header class="mifolyo-home-nav" aria-label="Primary navigation">
            <div class="mifolyo-home-nav-left">
                <a href="/" class="mifolyo-app-icon" aria-label="MiFolyo home">
                    <svg viewBox="0 0 24 24" role="img" aria-hidden="true">
                        <circle cx="12" cy="7" r="3"></circle>
                        <path d="M8 21V12a4 4 0 0 1 8 0v9"></path>
                    </svg>
                </a>
                <a href="#about">About</a>
                <a href="#how-it-works">How MiFolyo Works</a>
            </div>
            <div class="mifolyo-home-nav-actions">
                <button type="button" class="mifolyo-theme-button" aria-label="Toggle theme preview">
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                        <circle cx="12" cy="12" r="4"></circle>
                        <path d="M12 2v2M12 20v2M4 12H2M22 12h-2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19"></path>
                    </svg>
                </button>
                <a href="#sign-in" class="mifolyo-secondary-button">Sign in</a>
                <a href="#create-account" class="mifolyo-primary-button">Create account</a>
            </div>
        </header>

        <section class="mifolyo-hero" aria-labelledby="mifolyo-home-title">
            <div class="mifolyo-wordmark" aria-hidden="true">
                <svg viewBox="0 0 260 120" role="img">
                    <path class="mifolyo-mark-orange" d="M23 34c19-7 39-9 58-7M18 49c21 4 42 2 60-8M17 61c18 7 38 10 59 7M28 75c15 7 30 11 46 12"></path>
                    <circle class="mifolyo-mark-orange" cx="118" cy="32" r="12"></circle>
                    <path class="mifolyo-mark-orange" d="M104 88V55c0-8 6-14 14-14s14 6 14 14v33"></path>
                    <circle class="mifolyo-mark-blue" cx="165" cy="32" r="12"></circle>
                    <path class="mifolyo-mark-blue" d="M151 88V55c0-8 6-14 14-14s14 6 14 14v33"></path>
                    <circle class="mifolyo-mark-orange" cx="220" cy="31" r="12"></circle>
                    <path class="mifolyo-mark-blue" d="M198 50c12 18 32 28 52 31M197 63c15 6 32 9 49 8"></path>
                </svg>
                <h1 id="mifolyo-home-title">mifolyo</h1>
            </div>

            <p class="mifolyo-home-tagline">
                <span>Search-first.</span> <strong>Community-powered.</strong>
            </p>

            <form class="mifolyo-search-card" action="/api/search" method="GET" role="search">
                <label for="mifolyo-home-search" class="sr-only">Search the web and community</label>
                <svg class="mifolyo-search-icon" viewBox="0 0 24 24" aria-hidden="true">
                    <circle cx="11" cy="11" r="7"></circle>
                    <path d="m16.5 16.5 4 4"></path>
                </svg>
                <input id="mifolyo-home-search" name="q" type="search" placeholder="Search the web and community..." autocomplete="off">
                <button type="submit" aria-label="Search">
                    <svg viewBox="0 0 24 24" aria-hidden="true">
                        <circle cx="11" cy="11" r="7"></circle>
                        <path d="m16.5 16.5 4 4"></path>
                    </svg>
                </button>
            </form>

            <div class="mifolyo-mode-switch" aria-label="Search mode preview">
                <div class="mifolyo-mode-tabs">
                    <span>MIFOLYO</span>
                    <a href="/" class="active">Home</a>
                    <a href="/api/search">Search</a>
                    <a href="/discussions?url=https%3A%2F%2Fexample.com">Thread</a>
                </div>
                <div class="mifolyo-mode-bottom">
                    <span class="active">Search</span>
                    <span>Community</span>
                </div>
            </div>
        </section>

        <section id="about" class="mifolyo-home-copy">
            <h2>Search with context.</h2>
            <p>MiFolyo combines web discovery with source-level community discussion, so every result can collect questions, caveats, and trusted context over time.</p>
        </section>

        <section id="how-it-works" class="mifolyo-home-copy">
            <h2>How MiFolyo works.</h2>
            <p>Search the web, open discussions on any result, and let community threads surface what a link alone cannot explain.</p>
        </section>
    </main>
</body>

</html>
