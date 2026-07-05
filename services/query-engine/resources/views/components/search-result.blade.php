<li class="result-container">
    @php
        $discussionUrl = Str::startsWith($url, ['http://', 'https://']) ? $url : 'https://' . $url;
        $discussionPage = '/discussions?url=' . urlencode($discussionUrl) . '&title=' . urlencode($title);
    @endphp
    <div class="result-card-content">
        <a href="{{ $discussionUrl }}" class="result-card-main">
            <p class="result-url">{{ $url }}</p>
            <h2 class="result-title">{{ $title }}</h2>
            <p class="result-text">{{ Str::limit($text, 220) }}</p>
        </a>
        <div class="result-actions">
            <button type="button" class="btn-discuss" data-discuss-url="{{ $discussionUrl }}"
                data-discuss-title="{{ $title }}" data-discuss-page="{{ $discussionPage }}">
                Discuss
            </button>
            <a href="/api/page-connections/?url={{ urlencode($url) }}" target="_blank" class="btn-connection"
                title="Open page's connections">
                View Page's Links
                <i class="fa-solid fa-arrow-up-right-from-square"></i>
            </a>
        </div>
    </div>
</li>
