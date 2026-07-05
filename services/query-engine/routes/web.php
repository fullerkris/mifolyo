<?php

use Illuminate\Support\Facades\Route;
use Illuminate\Http\Request;

Route::get('/', function () {
    return view('home');
});

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
