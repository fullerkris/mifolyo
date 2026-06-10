<?php

use App\Http\Middleware\FuzzySearch;
use App\Http\Middleware\StoreSearchTerm;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Route;
use Illuminate\Support\Facades\DB;

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

// Return a secret message when the url is /secret
Route::get('/secret', function () {
    return response()->json(['message' => 'Congratulations! You have found the secret message! It does nothing :)']);
});
