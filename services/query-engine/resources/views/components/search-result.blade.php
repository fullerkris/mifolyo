<li class="result-container relative">
    @php
        $discussionUrl = Str::startsWith($url, ['http://', 'https://']) ? $url : 'https://' . $url;
    @endphp
    <!-- Full clickable area -->
    <div class="flex flex-col">
        <a href="{{ $discussionUrl }}" class="mb-2">
            <div class="flex justify-between items-start">
                <h3 class="result-title">{{ $title }}</h3>
            </div>
            <p class="result-text mt-2">{{ Str::limit($text, 200) }}</p>
            <p class="result-url text-sm text-gray-500 mt-1">{{ $url }}</p>
        </a>
        <div class="result-actions">
            <button type="button" class="btn-discuss" data-discuss-url="{{ $discussionUrl }}"
                data-discuss-title="{{ $title }}">
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
