<?php

namespace App\Http\Middleware;

use App\Services\ForumApi;
use App\Services\Onboarding;
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
                    Onboarding::receive($request, $response->json('data.onboarding'));
                }
            } catch (HttpExceptionInterface) {
                // An unavailable forum must not prevent browsing the search index.
            }
        }

        $response = $next($request)->header('Cache-Control', 'private, no-store');
        if ($response->isSuccessful() && $request->isMethod('GET') && ! $request->expectsJson()) {
            $state = Onboarding::state($request);
            if ($state['welcome'] && Onboarding::isSearch($request)) {
                app(Onboarding::class)->complete($request, 'welcome');
            }
            if ($state['discuss'] && $request->routeIs('discussions', 'threads.show')) {
                app(Onboarding::class)->complete($request, 'discuss');
            }
        }
        if ($request->is('forgot-password', 'reset-password', 'account')) {
            $response->headers->set('Referrer-Policy', 'no-referrer');
        }

        return $response;
    }
}
