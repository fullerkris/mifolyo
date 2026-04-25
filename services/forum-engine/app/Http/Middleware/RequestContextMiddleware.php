<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Str;
use Symfony\Component\HttpFoundation\Response;

class RequestContextMiddleware
{
    public function handle(Request $request, Closure $next): Response
    {
        $requestId = (string) ($request->headers->get('X-Request-Id') ?: Str::uuid());

        $request->attributes->set('request_id', $requestId);

        Log::withContext([
            'request_id' => $requestId,
            'request_method' => $request->method(),
            'request_path' => '/'.$request->path(),
            'request_ip' => $request->ip(),
        ]);

        $start = microtime(true);
        $response = $next($request);
        $durationMs = (int) round((microtime(true) - $start) * 1000);
        $userId = $request->user('api')?->id ?? $request->user()?->id;

        Log::info('request.completed', [
            'request_id' => $requestId,
            'user_id' => $userId,
            'status_code' => $response->getStatusCode(),
            'duration_ms' => $durationMs,
        ]);

        $response->headers->set('X-Request-Id', $requestId);

        return $response;
    }
}
