<?php

namespace App\Support;

use Illuminate\Http\Request;

class SearchContext
{
    public static function url(mixed $value, bool $allowImages = true): ?string
    {
        if (! is_string($value) || strlen($value) > 8192 || preg_match('/[\\\\\x00-\x20\x7f]/', $value)) {
            return null;
        }
        $parts = parse_url($value);
        if ($parts === false || isset($parts['scheme']) || isset($parts['host']) || isset($parts['fragment'])
            || ! in_array($parts['path'] ?? '', $allowImages ? ['/search', '/search/exact', '/images'] : ['/search', '/search/exact'], true)) {
            return null;
        }
        parse_str($parts['query'] ?? '', $query);
        if (! is_string($query['q'] ?? null) || ! mb_check_encoding($query['q'], 'UTF-8') || mb_strlen($query['q']) > 500) {
            return null;
        }
        $input = ['q' => $query['q']];
        if (isset($query['page'])) {
            $page = filter_var($query['page'], FILTER_VALIDATE_INT, ['options' => ['min_range' => 1, 'max_range' => 100]]);
            if ($page === false) {
                return null;
            }
            $input['page'] = $page;
        }
        // Keep one web-search origin through image/discussion journeys, never a nested return chain.
        if ($parts['path'] === '/images' && ($from = self::url($query['from'] ?? null, false))) {
            $input['from'] = $from;
        }

        return $parts['path'].'?'.http_build_query($input, '', '&', PHP_QUERY_RFC3986);
    }

    public static function parameters(Request $request): array
    {
        $url = self::url($request->input('from'));

        return $url === null ? [] : ['from' => $url];
    }
}
