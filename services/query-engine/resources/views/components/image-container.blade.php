<a href="{{ Str::startsWith($page_url, ['http://', 'https://']) ? $page_url : 'https://' . $page_url }}" target="_blank">
    <div class="image">
        <div class="flex justify-center w-full actual-image">
            <img src="{{ Str::startsWith($url, ['http://', 'https://']) ? $url : 'https://' . $url }}" alt="{{ $alt }}" class="w-full max-w-full object-contain">
        </div>
        <p class="img-title">{{ $title }}</p>
        <p class="img-text">{{ $text }}</p>
    </div>
</a>
