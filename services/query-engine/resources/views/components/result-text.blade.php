@props(['parts'])
@foreach ($parts as $part)
@if ($part['match'])<mark class="mf-result-match">{{ $part['text'] }}</mark>@else{{ $part['text'] }}@endif
@endforeach
