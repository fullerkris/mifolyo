<?php

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\Validator;
use Illuminate\Validation\Rule;

Route::get('/', function () {
    return view('home');
})->name('home');

Route::get('/explore', function (Request $request) {
    $mode = $request->query('mode', 'search');
    $query = $request->query('q', '');
    $input = [
        'mode' => $mode,
        'q' => is_string($query) ? trim($query) : $query,
    ];
    $isCommunityMode = $input['mode'] === 'community';

    $validator = Validator::make($input, [
        'mode' => ['bail', 'required', 'string', Rule::in(['search', 'community'])],
        'q' => $isCommunityMode
            ? ['bail', 'required', 'string', 'url:http,https', 'max:2048']
            : ['bail', 'required', 'string', 'max:500'],
    ], [
        'mode.in' => 'Choose Search or Community mode.',
        'q.required' => $isCommunityMode
            ? 'Enter a page URL to view its discussions.'
            : 'Enter something to search for.',
        'q.url' => 'Enter a valid HTTP or HTTPS page URL.',
    ]);

    if ($validator->fails()) {
        return redirect()->route('home')->withErrors($validator)->withInput([
            'mode' => is_string($input['mode']) ? $input['mode'] : 'search',
            'q' => is_string($input['q']) ? $input['q'] : '',
        ]);
    }

    $validated = $validator->validated();

    if ($isCommunityMode) {
        return redirect()->route('discussions', ['url' => $validated['q']]);
    }

    return redirect('/api/search?'.http_build_query(
        ['q' => $validated['q']],
        '',
        '&',
        PHP_QUERY_RFC3986,
    ));
})->name('home.explore');

Route::get('/discussions', function (Request $request) {
    $validated = $request->validate([
        'url' => ['required', 'url:http,https', 'max:2048'],
        'title' => ['nullable', 'string', 'max:300'],
    ]);

    return view('discussions', [
        'sourceUrl' => $validated['url'],
        'sourceTitle' => $validated['title'] ?? null,
    ]);
})->name('discussions');
