<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Redis;
use Symfony\Component\HttpFoundation\Response;
use Throwable;

class StoreSearchTerm
{
    /**
     * Handle an incoming request.
     *
     * @param  \Closure(\Illuminate\Http\Request): (\Symfony\Component\HttpFoundation\Response)  $next
     */
    public function handle(Request $request, Closure $next): Response
    {
        // Count searches without persisting their content.
        $searchTerm = $request->attributes->get('processedQuery');

        if ($searchTerm === null || $searchTerm === '') {
            return $next($request);
        }
        try {
            Redis::incr('total_searches');
            Redis::expire('total_searches', 86400);
        } catch (Throwable) {
            Log::warning('Search telemetry unavailable.');
        }

        return $next($request);
    }
}
