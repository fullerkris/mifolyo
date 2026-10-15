<?php

use App\Http\Controllers\BrowserForumController;
use App\Http\Controllers\ForumAccountController;
use App\Http\Controllers\QuerySearchController;
use App\Http\Controllers\ThreadPageController;
use App\Http\Middleware\FuzzySearch;
use App\Http\Middleware\StoreSearchTerm;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\Validator;
use Illuminate\Validation\Rule;

Route::get('/', function () {
    return view('home');
})->name('home');

Route::get('/sign-in', [ForumAccountController::class, 'form'])->name('account.sign-in');
Route::post('/sign-in', [ForumAccountController::class, 'authenticate'])->middleware('throttle:10,1,forum-auth')->name('account.sign-in.submit');
Route::get('/register', [ForumAccountController::class, 'form'])->name('account.register');
Route::post('/register', [ForumAccountController::class, 'authenticate'])->middleware('throttle:10,1,forum-auth')->name('account.register.submit');
Route::post('/sign-out', [ForumAccountController::class, 'logout'])->name('account.sign-out');
Route::get('/threads/{thread}', [ThreadPageController::class, 'show'])->whereNumber('thread')->name('threads.show');
Route::post('/threads/{thread}/comments', [ThreadPageController::class, 'comment'])->whereNumber('thread')->middleware('throttle:30,1,forum-write')->name('threads.comment');
Route::get('/community/threads', [BrowserForumController::class, 'index'])->middleware('throttle:90,1,forum-read')->name('community.threads');
Route::post('/community/thread-counts', [BrowserForumController::class, 'counts'])->middleware('throttle:90,1,forum-read')->name('community.counts');
Route::post('/community/threads', [BrowserForumController::class, 'create'])->middleware('throttle:30,1,forum-write')->name('community.create');

Route::get('/search', [QuerySearchController::class, 'search'])
    ->middleware(['throttle:30,1', FuzzySearch::class, StoreSearchTerm::class])->name('search');
Route::get('/search/exact', [QuerySearchController::class, 'search'])
    ->middleware('throttle:30,1')->name('search.exact');

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

    return redirect('/search?'.http_build_query(
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
