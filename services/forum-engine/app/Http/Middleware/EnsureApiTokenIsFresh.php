<?php

namespace App\Http\Middleware;

use App\Models\User;
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
            User::query()->whereKey($user->id)->where('api_token', $user->api_token)->update([
                'api_token' => null,
                'api_token_expires_at' => null,
                'api_token_last_used_at' => null,
            ]);

            return response()->json([
                'message' => 'Token expired.',
            ], 401);
        }

        User::query()->whereKey($user->id)->where('api_token', $user->api_token)->update([
            'api_token_last_used_at' => now(),
        ]);

        return $next($request);
    }
}
