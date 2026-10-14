<?php

namespace App\Http\Middleware;

use App\Services\ForumApi;
use Closure;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class ResolveForumAccount
{
    public function handle(Request $request, Closure $next)
    {
        if ($request->session()->has('forum.token')) {
            try {
                $response = app(ForumApi::class)->call($request, 'get', '/api/auth/me');
                if ($response->successful() && is_array($response->json('data')) && $response->json('data.id')) {
                    $request->session()->put('forum.user', array_intersect_key($response->json('data'), array_flip(['id', 'username', 'name'])));
                }
            } catch (HttpExceptionInterface) {
                // An unavailable forum must not prevent browsing the search index.
            }
        }

        return $next($request)->header('Cache-Control', 'private, no-store');
    }
}
