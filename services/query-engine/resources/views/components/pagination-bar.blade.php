@if ($totalPages > 1)
    @php
        $query = request()->query('q', '');
        $pageUrl = fn ($number) => request()->url().'?'.http_build_query(['q' => is_string($query) ? $query : '', 'page' => $number], '', '&', PHP_QUERY_RFC3986);
    @endphp
    <nav class="mf-pagination" aria-label="Search result pages">
        @if ($currentPage > 1)
            <a href="{{ $pageUrl(min($totalPages, $currentPage - 1)) }}" rel="prev" class="mf-pagination-direction">← Previous</a>
        @endif
        @if ($pages[0] > 1)
            <a href="{{ $pageUrl(1) }}" aria-label="Page 1">1</a>
            @if ($pages[0] > 2)<span aria-hidden="true">…</span>@endif
        @endif
        @foreach ($pages as $number)
            @if ($number === $currentPage)
                <span aria-current="page" aria-label="Page {{ $number }}">{{ $number }}</span>
            @else
                <a href="{{ $pageUrl($number) }}" aria-label="Page {{ $number }}">{{ $number }}</a>
            @endif
        @endforeach
        @if (end($pages) < $totalPages)
            @if (end($pages) < $totalPages - 1)<span aria-hidden="true">…</span>@endif
            <a href="{{ $pageUrl($totalPages) }}" aria-label="Page {{ $totalPages }}">{{ $totalPages }}</a>
        @endif
        @if ($currentPage < $totalPages)
            <a href="{{ $pageUrl($currentPage + 1) }}" rel="next" class="mf-pagination-direction">Next →</a>
        @endif
    </nav>
@endif
