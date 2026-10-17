<?php

// Explicit test-server entrypoint. Production public/index.php never loads this file.
if (PHP_SAPI !== 'cli-server' || getenv('APP_ENV') !== 'testing' || getenv('SEARCH_UI_BROWSER_FIXTURES') !== '1') {
    http_response_code(404);
    exit;
}

$public = realpath(__DIR__.'/../../public');
$asset = realpath($public.parse_url($_SERVER['REQUEST_URI'], PHP_URL_PATH));
if ($asset !== false && str_starts_with($asset, $public.DIRECTORY_SEPARATOR) && is_file($asset)) {
    return false;
}

require __DIR__.'/../../vendor/autoload.php';
$app = require __DIR__.'/../../bootstrap/app.php';
$kernel = $app->make(Illuminate\Contracts\Http\Kernel::class);
$kernel->bootstrap();

// Counts use the real CSRF-protected browser route and need a session across requests.
config(['session.driver' => 'file', 'session.cookie' => 'mifolyo_browser_fixture_session']);

// Deterministic UI data; the separate MongoDB feature tests cover actual query execution.
foreach ([App\Http\Middleware\FuzzySearch::class, App\Http\Middleware\StoreSearchTerm::class] as $middleware) {
    $app->instance($middleware, new class
    {
        public function handle($request, $next)
        {
            return $next($request);
        }
    });
}
$app->instance(App\Http\Controllers\QuerySearchController::class, new class extends App\Http\Controllers\QuerySearchController
{
    public function getTopImages($query, $page = 1, $perPage = 5)
    {
        if ($query === 'unavailable') {
            throw new MongoDB\Driver\Exception\ConnectionTimeoutException('Fixture index unavailable');
        }
        if ($query === 'rate-limited') {
            abort(429, '', ['Retry-After' => '60']);
        }
        $total = in_array($query, ['', 'no-matches'], true) ? 0 : 25;
        $results = [];
        for ($number = ($page - 1) * $perPage + 1; $number <= min($total, $page * $perPage); $number++) {
            $results[] = [
                '_id' => request()->getSchemeAndHttpHost().'/mifolyo-mark.svg?image='.$number,
                'page_url' => 'https://example.test/guide/'.$number.'?ref=search&lang=en',
                'alt' => 'MiFolyo sample image '.$number, 'page_title' => 'A visual guide to community search · '.$number,
                'page_text' => 'A synthetic indexed image associated with this exact source page.',
            ];
        }
        if ($query === 'metadata') {
            $results[0]['page_title'] = '<img src=x onerror=window.unsafeMetadata=1>';
            $results[0]['alt'] = '<svg onload=window.unsafeMetadata=1>';
            $results[1]['_id'] = 'javascript:alert(1)';
            $results[1]['page_url'] = 'data:text/html,bad';
            $results[1]['page_title'] = $results[1]['alt'] = [];
            $results[2]['_id'] = request()->getSchemeAndHttpHost().'/missing-preview.png';
        }

        return [$results, $total];
    }

    public function get_page_connections(Illuminate\Http\Request $request)
    {
        $input = $request->validate(['url' => 'nullable|string|url:http,https|max:2048', 'direction' => 'sometimes|in:outgoing,incoming', 'page' => 'sometimes|integer|min:1|max:100']);
        $url = $input['url'] ?? '';
        if (str_contains($url, '/unavailable')) {
            throw new MongoDB\Driver\Exception\ConnectionTimeoutException('Fixture index unavailable');
        }
        if (str_contains($url, '/rate-limited')) {
            abort(429, '', ['Retry-After' => '60']);
        }
        $direction = $input['direction'] ?? 'outgoing';
        $page = (int) ($input['page'] ?? 1);
        $title = 'An indexed source <em>with literal metadata</em>';
        $counts = ['outgoing' => $url === '' || str_contains($url, '/empty') ? 0 : 25, 'incoming' => 2];
        $connections = [];
        for ($number = ($page - 1) * 20 + 1; $number <= min($counts[$direction], $page * 20); $number++) {
            $connections[] = ['url' => 'https://example.test/linked/'.$number.'?b=2&a=1#part', 'title' => 'Connected page '.$number, 'outgoing' => true, 'incoming' => $number <= 2];
        }
        $mapLinks = $connections;
        $parameters = ['url' => $url, 'direction' => $direction] + App\Support\SearchContext::parameters($request);

        return view('page-connections', compact('url', 'title', 'counts', 'direction', 'page', 'connections', 'mapLinks', 'parameters'));
    }

    public function search(Illuminate\Http\Request $request)
    {
        $input = $request->validate(['q' => 'nullable|string|max:500', 'page' => 'sometimes|integer|min:1|max:100']);
        $query = $input['q'] ?? '';
        if ($query === 'unavailable') {
            throw new MongoDB\Driver\Exception\ConnectionTimeoutException('Fixture index unavailable');
        }
        if ($query === 'rate-limited') {
            abort(429, '', ['Retry-After' => '60']);
        }
        $page = (int) ($input['page'] ?? 1);
        $total = in_array($query, ['', 'no-matches'], true) ? 0 : 45;
        $samples = [
            ['A practical guide to community search', 'Useful search starts with a good source. Explore how source-linked discussions help readers compare experiences, ask questions, and add context.'],
            ['Understanding independent web indexes', 'A closer look at how a focused web index is built, how pages become discoverable, and why the original source still matters.'],
            ['Designing meaningful source discussions', 'Practical ideas for keeping conversations grounded in evidence while making room for different perspectives and thoughtful questions.'],
        ];
        $results = [];
        for ($number = ($page - 1) * 20 + 1; $number <= min($total, $page * 20); $number++) {
            [$title, $summary] = $samples[($number - 1) % count($samples)];
            $results[] = ['_id' => 'https://example.test/guide/'.$number.'?ref=search&lang=en', 'title' => $title.' · '.$number, 'summary_text' => $summary];
        }

        if ($query === 'café C++') {
            $results = [
                ['_id' => 'https://example.test/caf%C3%A9?topic=c%2B%2B&lang=fr#examples', 'title' => 'CAFÉ & C++ <img src=x onerror=window.unsafeMetadata=1>',
                    'summary_text' => str_repeat('Opening material without a match. ', 25).'A useful café guide explains C++ and <svg onload=window.unsafeMetadata=1>. '.str_repeat('Further context follows. ', 20)],
                ['_id' => 'https://example.test/'.str_repeat('long-path/', 20).'?b=two&a=1#section', 'title' => str_repeat('A long café metadata title with useful detail ', 12), 'summary_text' => str_repeat('unbroken', 100)],
                ['_id' => 'https://example.test/fallback', 'title' => null, 'summary_text' => " \n\t", 'description' => 'A description about café and C++.'],
                ['_id' => 'javascript:alert(1)', 'title' => [], 'summary_text' => [], 'description' => null],
                ['_id' => 'https://example.test/relevant', 'title' => 'Choosing an excerpt', 'summary_text' => 'An unrelated site introduction.', 'description' => 'A relevant café and C++ description.'],
                ['_id' => 'https://example.test/zero', 'title' => '0', 'summary_text' => '0'],
            ];
            $total = count($results);
        }

        return view('search-results', [
            'query' => $query, 'originalQuery' => $query, 'page' => $page, 'total' => $total,
            'results' => $results, 'topImages' => $query === 'sidebar-preview' ? [
                ['_id' => request()->getSchemeAndHttpHost().'/missing-preview.png', 'page_url' => 'https://example.test/images?b=2&a=1#source', 'page_title' => '<em>A captured image source</em>', 'alt' => []],
                ['_id' => request()->getSchemeAndHttpHost().'/mifolyo-mark.svg', 'page_url' => 'https://example.test/working', 'page_title' => 'Working preview', 'alt' => ''],
            ] : [], 'suggestions' => false,
        ]);
    }
});
Illuminate\Support\Facades\Http::preventStrayRequests();
Illuminate\Support\Facades\Http::fake([
    '*/api/auth/login' => Illuminate\Support\Facades\Http::response(['data' => ['user' => ['id' => 3, 'username' => 'reader'], 'token' => 'browser-fixture-server-token']]),
    '*/api/auth/me' => Illuminate\Support\Facades\Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
    '*/api/auth/logout' => Illuminate\Support\Facades\Http::response(['message' => 'Signed out']),
    '*/api/votes' => function ($request) {
        $type = $request['votable_type'];
        $id = (int) $request['votable_id'];
        $value = (int) $request['value'];
        request()->session()->put('fixture_votes.'.$type.'.'.$id, $value);
        $base = $type === 'post' ? 4 : 2;

        return Illuminate\Support\Facades\Http::response(['data' => ['viewer_vote' => $value, 'votable' => ['type' => $type, 'id' => $id, 'score' => $base + $value]]]);
    },
    '*/api/threads/counts' => fn ($request) => Illuminate\Support\Facades\Http::response(['data' => array_map(
        fn ($url) => ['url' => $url, 'count' => str_contains($url, '/guide/1?') ? 2 : 0], $request['urls']
    )]),
    '*/api/posts/1' => fn () => Illuminate\Support\Facades\Http::response(['data' => [
        'id' => 1, 'title' => 'What makes this source useful?', 'body' => 'A full conversation grounded in the original source.',
        'source_url' => 'https://example.test/source', 'comment_count' => 2, 'author' => ['username' => 'reader', 'level' => 4],
        'score' => 4 + session('fixture_votes.post.1', 0), 'viewer_vote' => session('fixture_votes.post.1', 0),
    ]]),
    '*/api/posts/1/comments' => fn () => Illuminate\Support\Facades\Http::response(['data' => [
        ['id' => 4, 'body' => 'A thoughtful question', 'score' => 2 + session('fixture_votes.comment.4', 0), 'viewer_vote' => session('fixture_votes.comment.4', 0), 'author' => ['username' => 'reader', 'level' => 4], 'depth' => 0, 'children' => [
            ['id' => 5, 'body' => 'A nested perspective', 'score' => 2 + session('fixture_votes.comment.5', 0), 'viewer_vote' => session('fixture_votes.comment.5', 0), 'author' => ['username' => 'curious', 'level' => 2], 'depth' => 1, 'children' => []],
        ]],
        ['id' => 6, 'body' => 'A locked comment', 'score' => 0, 'viewer_vote' => 0, 'is_locked' => true, 'children' => []],
        ['id' => 7, 'body' => '[Comment removed]', 'is_removed' => true, 'children' => []],
    ]]),
    '*' => Illuminate\Support\Facades\Http::response([
        'data' => [
            ['id' => 1, 'title' => 'What makes this source useful?', 'body' => 'The examples give us a starting point. I would like to compare them with other sources and hear which details helped people most.', 'author' => ['username' => 'reader', 'level' => 4], 'community' => ['name' => 'Reference'], 'score' => 4, 'comment_count' => 2],
            ['id' => 2, 'title' => 'Another perspective on the same page', 'body' => 'This is a deterministic browser-test conversation, attached to the selected source.', 'author' => ['username' => 'curious', 'level' => 2], 'score' => 2, 'comment_count' => 1],
        ],
        'meta' => ['total' => 2, 'current_page' => 1, 'last_page' => 1],
    ]),
]);

$request = Illuminate\Http\Request::capture();
$response = $kernel->handle($request);
$response->headers->set('X-MiFolyo-Browser-Fixture', '1');
$response->send();
$kernel->terminate($request, $response);
