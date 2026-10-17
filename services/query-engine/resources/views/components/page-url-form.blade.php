@props(['url' => ''])
<form class="mf-page-url-form" method="GET" action="{{ route('page.links') }}" data-search-form data-navigation-message="Exploring page links…">
    <label for="page-url">Explore a page’s connections</label>
    <div>
        <input id="page-url" name="url" type="url" maxlength="2048" value="{{ $url }}" placeholder="https://example.com/article" required>
        @foreach (\App\Support\SearchContext::parameters(request()) as $name => $value)<input type="hidden" name="{{ $name }}" value="{{ $value }}">@endforeach
        <button class="mf-button mf-primary" type="submit">Explore links</button>
    </div>
</form>
