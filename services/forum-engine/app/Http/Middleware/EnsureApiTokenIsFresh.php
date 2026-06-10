<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpFoundation\Response;

class EnsureApiTokenIsFresh
{
    public function handle(Request $request, Closure $next): Response
    {
        $user = $request->user('api');

        if (! $user) {
            return $next($request);
        }

        if ($user->api_token_expires_at && $user->api_token_expires_at->isPast()) {
            $user->forceFill([
                'api_token' => null,
                'api_token_expires_at' => null,
                'api_token_last_used_at' => null,
            ])->saveQuietly();

            return response()->json([
                'message' => 'Token expired.',
            ], 401);
        }

        $user->forceFill([
            'api_token_last_used_at' => now(),
        ])->saveQuietly();

        return $next($request);
    }
}
