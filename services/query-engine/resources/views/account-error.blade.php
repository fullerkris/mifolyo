<x-search-layout :title="$heading.' — MiFolyo'" :home="true">
    <main id="main" class="mf-page mf-account-page" tabindex="-1">
        <section class="mf-empty-state mf-error-state" role="alert">
            <h1>{{ $heading }}</h1>
            <p>{{ $message }}</p>
            <a class="mf-button mf-primary" href="{{ $retryUrl }}">Return to form</a>
        </section>
    </main>
</x-search-layout>
