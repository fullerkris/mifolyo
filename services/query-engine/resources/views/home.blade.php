<x-search-layout :home="true">
    <main id="main" class="mf-home" tabindex="-1">
        <section class="mf-hero" aria-labelledby="home-title">
            <div class="mf-hero-brand" aria-hidden="true">
                <img src="/mifolyo-mark.svg" width="280" height="90" alt="">
                <span>mifolyo</span>
            </div>
            <h1 id="home-title"><span>Search-first.</span> <em>Community-powered.</em></h1>
            <p class="mf-hero-subtitle">Find a useful source. Discover another perspective.</p>
            <x-search-form :hero="true" :query="old('q', '')" />
            @error('q')
                <p class="mf-form-error" role="alert">{{ $message }}</p>
            @enderror
            <p class="mf-search-hint">Search indexed pages by keyword, then open a result’s discussions.</p>
        </section>
        <section id="how-it-works" class="mf-how" aria-labelledby="how-title">
            <div class="mf-section-heading">
                <p class="mf-eyebrow">A little context goes a long way</p>
                <h2 id="how-title">Good conversations start with a source.</h2>
            </div>
            <ol class="mf-steps">
                <li><span class="mf-step-number">01</span><h3>Follow your curiosity</h3><p>Search for a topic, a question, or something you want to understand better.</p></li>
                <li><span class="mf-step-number">02</span><h3>Go to the source</h3><p>Explore indexed pages and open the original website to read the full story.</p></li>
                <li><span class="mf-step-number">03</span><h3>Find another perspective</h3><p>Choose Discuss to read the community conversations attached to that exact page.</p></li>
            </ol>
        </section>
        <section id="about" class="mf-about">
            <x-search-icon name="chat" />
            <div><h2>The web has information. People add perspective.</h2><p>MiFolyo brings web discovery and source-level discussion together, so questions and context stay connected to the pages that inspired them.</p></div>
        </section>
    </main>
</x-search-layout>
