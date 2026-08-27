<?php

namespace Tests\Feature;

use App\Http\Controllers\QuerySearchController;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Log;
use Illuminate\Support\Facades\Redis;
use MongoDB\Client;
use MongoDB\Database;
use RuntimeException;
use Tests\TestCase;

class PageRankSearchTest extends TestCase
{
    private ?Client $mongoClient = null;

    private ?Database $mongoDatabase = null;

    protected function setUp(): void
    {
        parent::setUp();

        $uri = getenv('QUERY_MONGO_INTEGRATION_URI');
        if (! $uri) {
            $this->markTestSkipped('QUERY_MONGO_INTEGRATION_URI is not configured');
        }

        $databaseName = 'mifolyo_query_ranking_'.bin2hex(random_bytes(8));
        $this->mongoClient = new Client($uri);
        $this->mongoDatabase = $this->mongoClient->selectDatabase($databaseName);

        config([
            'database.connections.mongodb.dsn' => $uri,
            'database.connections.mongodb.database' => $databaseName,
        ]);
        DB::purge('mongodb');
    }

    protected function tearDown(): void
    {
        if ($this->mongoDatabase !== null) {
            $this->mongoDatabase->drop();
        }
        DB::purge('mongodb');
        $this->mongoClient = null;
        $this->mongoDatabase = null;

        parent::tearDown();
    }

    public function test_pagerank_scores_the_complete_candidate_set_before_pagination(): void
    {
        $primary = 'https://primary.test/';
        $promoted = 'https://promoted.test/';
        $words = [
            ['word' => 'alpha', 'url' => $primary, 'weight' => 0.1],
            ['word' => 'beta', 'url' => $primary, 'weight' => 0.1],
            ['word' => 'alpha', 'url' => $promoted, 'weight' => 0.9],
        ];
        $metadata = [
            $this->metadataDocument($primary, 'Primary'),
            $this->metadataDocument($promoted, 'Promoted'),
        ];
        for ($index = 0; $index < 20; $index++) {
            $url = sprintf('https://baseline-%02d.test/', $index);
            $words[] = ['word' => 'alpha', 'url' => $url, 'weight' => 1.0];
            $metadata[] = $this->metadataDocument($url, 'Baseline '.$index);
        }

        $this->mongoDatabase->selectCollection('words')->insertMany($words);
        $this->mongoDatabase->selectCollection('metadata')->insertMany($metadata);
        $this->mongoDatabase->selectCollection('pagerank')->insertOne([
            '_id' => $promoted,
            'rank' => 1.0,
        ]);

        $response = $this->getJson('/api/search_force?q=alpha%2Bbeta');

        $response
            ->assertOk()
            ->assertJsonPath('meta.total', 22)
            ->assertJsonCount(20, 'results')
            ->assertJsonPath('results.0._id', $primary)
            ->assertJsonPath('results.1._id', $promoted)
            ->assertJsonPath('results.1.title', 'Promoted');

        $promotedResult = $response->json('results.1');
        $this->assertEqualsWithDelta(1.0, $promotedResult['pagerank'], 1e-12);
        $this->assertEqualsWithDelta(0.94, $promotedResult['combinedScore'], 1e-12);
    }

    public function test_missing_zero_scores_have_deterministic_pagination(): void
    {
        $words = [];
        $metadata = [];
        $expectedURLs = [];
        for ($index = 0; $index < 21; $index++) {
            $url = sprintf('https://zero-%02d.test/', $index);
            $expectedURLs[] = $url;
            $words[] = ['word' => 'alpha', 'url' => $url, 'weight' => 0.0];
            $metadata[] = $this->metadataDocument($url, 'Zero '.$index);
        }
        $this->mongoDatabase->selectCollection('words')->insertMany($words);
        $this->mongoDatabase->selectCollection('metadata')->insertMany($metadata);

        $firstPage = $this->getJson('/api/search_force?q=alpha');
        $secondPage = $this->getJson('/api/search_force?q=alpha&page=2');

        $firstPage->assertOk()->assertJsonCount(20, 'results');
        $secondPage->assertOk()->assertJsonCount(1, 'results');
        $firstPageURLs = array_column($firstPage->json('results'), '_id');
        $secondPageURLs = array_column($secondPage->json('results'), '_id');
        $this->assertSame(array_slice($expectedURLs, 0, 20), $firstPageURLs);
        $this->assertSame(array_slice($expectedURLs, 20), $secondPageURLs);
        $this->assertSame([], array_intersect($firstPageURLs, $secondPageURLs));

        foreach (array_merge($firstPage->json('results'), $secondPage->json('results')) as $result) {
            $this->assertEqualsWithDelta(0.0, $result['pagerank'], 1e-12);
            $this->assertEqualsWithDelta(0.0, $result['combinedScore'], 1e-12);
        }
    }

    public function test_non_finite_scores_are_excluded_from_ranking(): void
    {
        $infiniteURL = 'https://infinite.test/';
        $nanURL = 'https://nan.test/';
        $this->mongoDatabase->selectCollection('words')->insertMany([
            ['word' => 'alpha', 'url' => $infiniteURL, 'weight' => INF],
            ['word' => 'alpha', 'url' => $nanURL, 'weight' => NAN],
        ]);
        $this->mongoDatabase->selectCollection('metadata')->insertMany([
            $this->metadataDocument($infiniteURL, 'Infinite'),
            $this->metadataDocument($nanURL, 'NaN'),
        ]);
        $this->mongoDatabase->selectCollection('pagerank')->insertMany([
            ['_id' => $infiniteURL, 'rank' => INF],
            ['_id' => $nanURL, 'rank' => NAN],
        ]);

        $response = $this->getJson('/api/search_force?q=alpha');

        $response->assertOk()->assertJsonCount(2, 'results');
        foreach ($response->json('results') as $result) {
            $this->assertEqualsWithDelta(0.0, $result['pagerank'], 1e-12);
            $this->assertEqualsWithDelta(0.0, $result['combinedScore'], 1e-12);
        }
    }

    public function test_image_association_results_keep_source_url_as_public_id(): void
    {
        $source = 'https://cdn.example.test/shared.jpg';
        $pageA = 'https://page-a.test/';
        $pageB = 'https://page-b.test/';
        $associationA = hash('sha256', $pageA."\0".$source);
        $associationB = hash('sha256', $pageB."\0".$source);
        $this->mongoDatabase->selectCollection('word_images')->insertMany([
            ['word' => 'shared', 'association_id' => $associationA, 'page_url' => $pageA, 'weight' => 30],
            ['word' => 'shared', 'association_id' => $associationB, 'page_url' => $pageB, 'weight' => 20],
        ]);
        $this->mongoDatabase->selectCollection('images')->insertMany([
            ['_id' => $associationA, 'source_url' => $source, 'page_url' => $pageA, 'alt' => 'A', 'filename' => 'shared.jpg'],
            ['_id' => $associationB, 'source_url' => $source, 'page_url' => $pageB, 'alt' => 'B', 'filename' => 'shared.jpg'],
        ]);
        $this->mongoDatabase->selectCollection('metadata')->insertMany([
            $this->metadataDocument($pageA, 'Page A'),
            $this->metadataDocument($pageB, 'Page B'),
        ]);

        [$results, $total] = $this->app->make(QuerySearchController::class)
            ->getTopImages('shared', 1, 20);

        $this->assertSame(2, $total);
        $this->assertCount(2, $results);
        $this->assertSame([$source, $source], array_column($results, '_id'));
        $this->assertEqualsCanonicalizing([$pageA, $pageB], array_column($results, 'page_url'));
        $this->assertArrayNotHasKey('association_id', $results[0]);
    }

    public function test_search_rejects_invalid_or_excessive_input(): void
    {
        $this->getJson('/api/search_force?q[]=alpha')->assertUnprocessable();
        $this->getJson('/api/search_force?q=alpha&page=0')->assertUnprocessable();
        $this->getJson('/api/search_force?q=%FF')->assertUnprocessable();
        $this->getJson('/api/search_force?'.http_build_query([
            'q' => implode(' ', array_fill(0, 21, 'term')),
        ]))->assertUnprocessable();

        $this->getJson('/api/search_force?q=0')
            ->assertOk()
            ->assertJsonPath('query', '0');
        $this->getJson('/api/search_images?q[]=alpha')->assertUnprocessable();
        $this->get('/api/search_images?q=0')->assertOk();
    }

    public function test_fuzzy_search_normalizes_and_matches_uppercase_accented_input(): void
    {
        $url = 'https://unicode.test/';
        $this->mongoDatabase->selectCollection('dictionary')->insertOne(['_id' => 'école']);
        $this->mongoDatabase->selectCollection('words')->insertOne([
            'word' => 'école',
            'url' => $url,
            'weight' => 1.0,
        ]);
        $this->mongoDatabase->selectCollection('metadata')->insertOne(
            $this->metadataDocument($url, 'Unicode'),
        );

        $this->getJson('/api/search?'.http_build_query(['q' => 'ÉCOLO']))
            ->assertOk()
            ->assertJsonPath('query', 'école')
            ->assertJsonPath('results.0._id', $url);
    }

    public function test_telemetry_write_failure_does_not_fail_search(): void
    {
        Redis::shouldReceive('incr')->once()->with('total_searches')->andThrow(new RuntimeException);
        Log::spy();

        $url = 'https://telemetry-failure.test/';
        $this->mongoDatabase->selectCollection('dictionary')->insertOne(['_id' => 'alpha']);
        $this->mongoDatabase->selectCollection('words')->insertOne([
            'word' => 'alpha',
            'url' => $url,
            'weight' => 1.0,
        ]);
        $this->mongoDatabase->selectCollection('metadata')->insertOne(
            $this->metadataDocument($url, 'Telemetry failure'),
        );

        $this->getJson('/api/search?q=alpha')
            ->assertOk()
            ->assertJsonPath('results.0._id', $url);
        Log::shouldHaveReceived('warning')->once()->with('Search telemetry unavailable.');
    }

    public function test_dictionary_returns_validated_pagination_metadata(): void
    {
        $this->mongoDatabase->selectCollection('dictionary')->insertMany([
            ['_id' => 'alpha'],
            ['_id' => 'beta'],
            ['_id' => 'delta'],
            ['_id' => 'epsilon'],
            ['_id' => 'gamma'],
        ]);

        $this->getJson('/api/dictionary?limit=2&page=2')
            ->assertOk()
            ->assertJsonPath('status', 'up')
            ->assertJsonPath('dictionary', ['delta', 'epsilon'])
            ->assertJsonPath('meta.total', 5)
            ->assertJsonPath('meta.page', 2)
            ->assertJsonPath('meta.per_page', 2)
            ->assertJsonPath('meta.last_page', 3);

        $this->getJson('/api/dictionary?limit=0')->assertUnprocessable();
        $this->getJson('/api/dictionary?page=0')->assertUnprocessable();
    }

    private function metadataDocument(string $url, string $title): array
    {
        return [
            '_id' => $url,
            'title' => $title,
            'description' => '',
            'last_crawled' => '',
            'summary_text' => '',
        ];
    }
}
