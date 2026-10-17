@props(['author' => null])
@php
    $username = is_array($author) && is_string($author['username'] ?? null) ? trim($author['username']) : '';
    $authorLevel = $username !== '' && is_int($author['level'] ?? null) && $author['level'] > 0 ? $author['level'] : null;
@endphp
<span class="mf-author-name">{{ $username !== '' ? $username : 'Community member' }}</span>@if ($authorLevel !== null) <span class="mf-author-level">Level {{ $authorLevel }}</span>@endif
