<x-search-layout :title="$heading.' — MiFolyo'">
    <main id="main" class="mf-page" tabindex="-1">
        <section class="mf-empty-state mf-error-state" role="alert">
            <x-search-icon name="chat" />
            <h1>{{ $heading }}</h1>
            <p>{{ $message }}</p>
            @if ($status === 401)
                <a class="mf-button mf-primary" href="{{ route('account.sign-in', ['return_to' => request()->getRequestUri()]) }}">Sign in</a>
            @elseif (in_array($status, [429, 503]))
                <a class="mf-button mf-primary" href="{{ request()->url() }}">Try again</a>
            @else
                <a class="mf-button mf-secondary" href="{{ route('home') }}">Back to search</a>
            @endif
        </section>
    </main>
</x-search-layout>
