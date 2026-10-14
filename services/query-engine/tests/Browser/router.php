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

        return view('search-results', [
            'query' => $query, 'originalQuery' => $query, 'page' => $page, 'total' => $total,
            'results' => $results, 'topImages' => [], 'suggestions' => false,
        ]);
    }
});
Illuminate\Support\Facades\Http::preventStrayRequests();
Illuminate\Support\Facades\Http::fake([
    '*/api/threads/counts' => fn ($request) => Illuminate\Support\Facades\Http::response(['data' => array_map(
        fn ($url) => ['url' => $url, 'count' => str_contains($url, '/guide/1?') ? 2 : 0], $request['urls']
    )]),
    '*/api/posts/1' => Illuminate\Support\Facades\Http::response(['data' => [
        'id' => 1, 'title' => 'What makes this source useful?', 'body' => 'A full conversation grounded in the original source.',
        'source_url' => 'https://example.test/source', 'comment_count' => 2, 'author' => ['username' => 'reader'],
    ]]),
    '*/api/posts/1/comments' => Illuminate\Support\Facades\Http::response(['data' => [
        ['id' => 4, 'body' => 'A thoughtful question', 'author' => ['username' => 'reader'], 'depth' => 0, 'children' => [
            ['id' => 5, 'body' => 'A nested perspective', 'author' => ['username' => 'curious'], 'depth' => 1, 'children' => []],
        ]],
    ]]),
    '*' => Illuminate\Support\Facades\Http::response([
        'data' => [
            ['id' => 1, 'title' => 'What makes this source useful?', 'body' => 'The examples give us a starting point. I would like to compare them with other sources and hear which details helped people most.', 'author' => ['username' => 'reader'], 'community' => ['name' => 'Reference'], 'score' => 4, 'comment_count' => 2],
            ['id' => 2, 'title' => 'Another perspective on the same page', 'body' => 'This is a deterministic browser-test conversation, attached to the selected source.', 'author' => ['username' => 'curious'], 'score' => 2, 'comment_count' => 1],
        ],
        'meta' => ['total' => 2, 'current_page' => 1, 'last_page' => 1],
    ]),
]);

$request = Illuminate\Http\Request::capture();
$response = $kernel->handle($request);
$response->headers->set('X-MiFolyo-Browser-Fixture', '1');
$response->send();
$kernel->terminate($request, $response);
