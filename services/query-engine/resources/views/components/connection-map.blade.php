@props(['url', 'label', 'links', 'direction'])
<details class="mf-connection-map" data-connection-map data-source-url="{{ $url }}" data-source-title="{{ $label }}">
    <summary>Connection map <span>Explore a visual overview</span></summary>
    <p class="mf-discovery-note">Showing the current {{ $direction }} page and up to 20 {{ $direction === 'outgoing' ? 'incoming' : 'outgoing' }} links. Use the lists below for full titles and keyboard navigation.</p>
    <ul hidden data-map-links>@foreach ($links as $link)<li data-url="{{ $link['url'] }}" data-title="{{ $link['title'] }}" data-outgoing="{{ $link['outgoing'] ? '1' : '0' }}" data-incoming="{{ $link['incoming'] ? '1' : '0' }}"></li>@endforeach</ul>
    <p data-map-status role="status">The interactive map uses JavaScript. All connections are available in the lists below.</p>
    <button class="mf-button mf-secondary" type="button" data-map-retry hidden>Reload page</button>
    <div class="mf-map-canvas" data-map-canvas role="img" aria-label="Visual map of the selected page and its connections" hidden></div>
    <div class="mf-map-toolbar" data-map-toolbar hidden>
        <button class="mf-button mf-secondary" type="button" data-map-fit>Fit map</button><button class="mf-button mf-secondary" type="button" data-map-zoom="in" aria-label="Zoom in">+</button><button class="mf-button mf-secondary" type="button" data-map-zoom="out" aria-label="Zoom out">−</button>
        <span class="mf-map-legend"><span><i class="mf-map-key mf-map-current"></i>Selected page</span><span><i class="mf-map-key mf-map-outgoing"></i>Outgoing</span><span><i class="mf-map-key mf-map-incoming"></i>Incoming</span><span><i class="mf-map-key mf-map-both"></i>Both ways</span></span>
    </div>
    <p class="mf-map-selection" data-map-selection hidden><span data-map-selected-title></span> <a class="mf-text-link" data-map-selected-link>Open selected page →</a></p>
</details>
