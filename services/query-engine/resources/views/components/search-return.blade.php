@php($searchReturn = \App\Support\SearchContext::url(request()->input('from')))
@if ($searchReturn)
    <a href="{{ $searchReturn }}#mf-return" class="mf-text-link mf-back-link" data-back-to-results>← Back to results</a>
@endif
