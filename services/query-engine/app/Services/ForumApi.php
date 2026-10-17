<?php

namespace App\Services;

use App\Support\SearchContext;
use Illuminate\Contracts\Encryption\DecryptException;
use Illuminate\Http\Client\ConnectionException;
use Illuminate\Http\Client\Response;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Symfony\Component\HttpKernel\Exception\HttpException;

class ForumApi
{
    public function call(Request $request, string $method, string $path, array $data = []): Response
    {
        $client = Http::baseUrl(config('services.forum.base_url'))->acceptJson()
            ->timeout(config('services.forum.timeout_seconds'))->connectTimeout(3)->withoutRedirecting();
        if ($encrypted = $request->session()->get('forum.token')) {
            try {
                $client = $client->withToken(Crypt::decryptString($encrypted));
            } catch (DecryptException) {
                $request->session()->forget('forum');
            }
        }
        try {
            $response = $client->{$method}($path, $data);
        } catch (ConnectionException) {
            throw new HttpException(503, 'The community service could not be reached.');
        }
        if ($response->status() === 401) {
            $request->session()->forget('forum');
        }

        return $response;
    }

    public function data(Response $response): array
    {
        abort_unless($response->successful(), in_array($response->status(), [401, 403, 404, 429], true) ? $response->status() : 503);
        $data = $response->json('data');
        abort_unless(is_array($data), 503);

        return $data;
    }

    public static function returnTo(mixed $path): string
    {
        if (is_string($path) && str_ends_with($path, '#mf-return')) {
            $search = SearchContext::url(substr($path, 0, -strlen('#mf-return')));

            return $search === null ? '/' : $search.'#mf-return';
        }
        if (is_string($path) && preg_match('~\A(/threads/[1-9][0-9]*(?:\?[^#]*)?)#(?:comment-[1-9][0-9]*|thread-votes)\z~', $path, $matches)) {
            return self::returnTo($matches[1]) === $matches[1] ? $path : '/';
        }

        // Only local reading journeys are valid authentication return targets.
        return is_string($path) && ! preg_match('/[\\\\\x00-\x20\x7f]/', $path)
            && preg_match('~\A/(?:threads/[1-9][0-9]*|discussions|search(?:/exact)?|images|page-links)?(?:\?[^#]*)?\z~', $path)
            ? $path : '/';
    }
}
