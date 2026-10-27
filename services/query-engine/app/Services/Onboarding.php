<?php

namespace App\Services;

use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class Onboarding
{
    public static function state(Request $request): array
    {
        return $request->attributes->get('onboarding', ['welcome' => false, 'discuss' => false, 'vote' => false]);
    }

    public static function receive(Request $request, mixed $state): bool
    {
        if (! is_array($state) || count($state) !== 3) {
            return false;
        }
        foreach (['welcome', 'discuss', 'vote'] as $step) {
            if (! is_bool($state[$step] ?? null)) {
                return false;
            }
        }
        $request->attributes->set('onboarding', $state);

        return true;
    }

    public function complete(Request $request, string $step): bool
    {
        if (! $request->session()->has(['forum.user', 'forum.token'])) {
            return false;
        }
        try {
            $response = app(ForumApi::class)->call($request, 'post', '/api/auth/onboarding', ['step' => $step]);

            return $response->successful() && self::receive($request, $response->json('data'))
                && self::state($request)[$step] === false;
        } catch (HttpExceptionInterface) {
            // Guidance must never turn a successful reading journey into an error.
            return false;
        }
    }

    public static function isSearch(Request $request): bool
    {
        return $request->routeIs('search', 'search.exact', 'search.images')
            && is_string($request->query('q')) && trim($request->query('q')) !== ''
            && ! $request->expectsJson();
    }
}
