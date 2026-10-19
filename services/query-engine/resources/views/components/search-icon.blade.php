@props(['name' => 'search'])
<svg {{ $attributes->class(['mf-icon']) }} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    @switch($name)
        @case('up')
            <path d="m5 13 7-7 7 7M12 6v14"/>
            @break
        @case('down')
            <path d="m5 11 7 7 7-7M12 4v14"/>
            @break
        @case('image')
            <rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="1.5"/><path d="m3 17 5-5 4 4 4-6 5 7"/>
            @break
        @case('links')
            <circle cx="12" cy="12" r="3"/><circle cx="4" cy="4" r="2"/><circle cx="20" cy="4" r="2"/><circle cx="20" cy="20" r="2"/><path d="m6 6 4 4m4 0 4-4m-4 8 4 4"/>
            @break
        @case('chat')
            <path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H3l2-5a8.5 8.5 0 1 1 16-3.5Z"/><path d="M8 10h8M8 14h5"/>
            @break
        @case('external')
            <path d="M14 3h7v7M21 3l-11 11M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>
            @break
        @case('theme')
            <circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>
            @break
        @case('close')
            <path d="m6 6 12 12M6 18 18 6"/>
            @break
        @default
            <circle cx="10.5" cy="10.5" r="7"/><path d="m16 16 5 5"/>
    @endswitch
</svg>
