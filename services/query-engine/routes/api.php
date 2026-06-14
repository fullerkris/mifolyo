<?php

use App\Http\Middleware\FuzzySearch;
use App\Http\Middleware\StoreSearchTerm;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;

use App\Http\Controllers\QuerySearchController;
use App\Http\Controllers\RedisController;

Route::get('/search', [QuerySearchController::class, 'search'])->middleware([FuzzySearch::class, StoreSearchTerm::class]);
Route::get('/search_images', [QuerySearchController::class, 'search_images']);
Route::get('/dictionary', [QuerySearchController::class, 'get_dictionary'])->name('get.dictionary');
Route::get('/search_force', [QuerySearchController::class, 'search'])->name('search_force');
Route::get('/search_images_force', [QuerySearchController::class, 'search_images'])->name('search_images_force');
Route::get('/stats', [QuerySearchController::class, 'stats'])->name('stats');
Route::get('/health/live', fn() => response()->json(['status' => 'up']));
Route::get('/health/ready', function () {
    DB::connection('mongodb')->table('metadata')->limit(1)->get();

    return response()->json(['status' => 'ready']);
});
Route::get('/get_top_searches', [RedisController::class, 'get_top_searches'])->name('get.top.searches');
Route::get('/get_search_suggestions', [RedisController::class, 'get_search_suggestions'])->name('get.search.suggestions');
Route::get('/cringe', [RedisController::class, 'cringe'])->name('cringe');
Route::get('/top_ranked_pages', [QuerySearchController::class, 'get_top_ranked_page'])->name('top_ranked_page');
Route::get('/page-connections', [QuerySearchController::class, 'get_page_connections'])->name('page_connections');
Route::get('/threads/by-url', function (Request $request) {
    $validated = $request->validate([
        'url' => ['required', 'url:http,https', 'max:2048'],
        'sort' => ['sometimes', 'in:top,new'],
        'per_page' => ['sometimes', 'integer', 'min:1', 'max:100'],
    ]);

    try {
        $response = Http::baseUrl(config('services.forum.base_url'))
            ->acceptJson()
            ->timeout(config('services.forum.timeout_seconds'))
            ->get('/api/threads/by-url', [
                'url' => $validated['url'],
                'sort' => $validated['sort'] ?? 'top',
                'per_page' => $validated['per_page'] ?? 20,
            ]);
    } catch (\Throwable) {
        return response()->json([
            'message' => 'Forum threads are unavailable right now.',
            'data' => [],
        ], 503);
    }

    return response($response->body(), $response->status())
        ->header('Content-Type', $response->header('Content-Type', 'application/json'));
});
Route::post('/threads', function (Request $request) {
    $validated = $request->validate([
        'community_slug' => ['sometimes', 'string', 'max:255'],
        'title' => ['required', 'string', 'max:300'],
        'body' => ['nullable', 'string', 'max:50000'],
        'source_url' => ['required', 'url:http,https', 'max:2048'],
    ]);

    $payload = [
        'community_slug' => $validated['community_slug'] ?? config('services.forum.default_community_slug'),
        'title' => $validated['title'],
        'body' => $validated['body'] ?? null,
        'source_url' => $validated['source_url'],
    ];

    try {
        $client = Http::baseUrl(config('services.forum.base_url'))
            ->acceptJson()
            ->timeout(config('services.forum.timeout_seconds'));

        if ($request->bearerToken()) {
            $client = $client->withToken($request->bearerToken());
        }

        $response = $client->post('/api/threads', $payload);
    } catch (\Throwable) {
        return response()->json([
            'message' => 'Forum threads are unavailable right now.',
            'data' => null,
        ], 503);
    }

    return response($response->body(), $response->status())
        ->header('Content-Type', $response->header('Content-Type', 'application/json'));
});

// Return a secret message when the url is /secret
Route::get('/secret', function () {
    return response()->json(['message' => 'Congratulations! You have found the secret message! It does nothing :)']);
});
