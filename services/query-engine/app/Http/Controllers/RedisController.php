<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Support\Facades\Cache;
use Illuminate\Support\Facades\Redis;

class RedisController extends Controller
{
    public function get_top_searches(Request $request)
    {
        // Raw-term telemetry is retired. Do not read legacy values back out.
        return response()->json(['searches' => []]);
    }

    public function get_search_suggestions(Request $request)
    {
        // Never expose terms retained by an older release.
        return response()->json(['searches' => []]);
    }

    public function cringe(Request $request)
    {

        error_log('Cringe function called');
        // Legacy raw terms are deliberately not read or displayed.
        $topSearches = [];

        // Fetch the number of searches performed from Redis
        $totalSearches = Redis::get('total_searches');

        // Call the get_random_page function from QuerySearchController
        $querySearchController = new QuerySearchController;

        // Page of the day
        // Check if the random page is cached in Laravel
        if (Cache::has('random_page')) {
            $randomPage = Cache::get('random_page');
        } else {
            // If not cached, fetch the random page from Redis
            $randomPage = $querySearchController->get_random_page($request);
            // Cache the random page for 24 hours
            Cache::put('random_page', $randomPage, 1440);
        }

        // Return view
        return view('cringe-results', [
            'topSearches' => $topSearches,
            'totalSearches' => $totalSearches,
            'randomPage' => $randomPage,
        ]);
    }
}
