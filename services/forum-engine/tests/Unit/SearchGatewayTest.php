<?php

namespace Tests\Unit;

use App\Services\SearchGateway;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class SearchGatewayTest extends TestCase
{
    public function test_search_web_returns_results_when_upstream_is_healthy(): void
    {
        Http::fake([
            'search.internal/api/search*' => Http::response([
                'results' => [
                    ['title' => 'Result 1'],
                    ['title' => 'Result 2'],
                ],
                'meta' => ['source' => 'external-search'],
            ], 200),
        ]);

        $gateway = new SearchGateway([
            'base_url' => 'http://search.internal',
            'timeout_seconds' => 2,
            'connect_timeout_seconds' => 1,
            'retry_attempts' => 2,
            'retry_delay_ms' => 10,
        ]);

        $result = $gateway->searchWeb('mifolyo', page: 2, perPage: 5);

        $this->assertSame('ok', $result['status']);
        $this->assertCount(2, $result['results']);
        $this->assertSame('external-search', $result['meta']['source']);

        Http::assertSent(function ($request): bool {
            return $request->url() === 'http://search.internal/api/search?q=mifolyo&page=2&per_page=5';
        });
    }

    public function test_search_web_retries_and_recovers_from_transient_failure(): void
    {
        Http::fakeSequence()
            ->push(['message' => 'temporary failure'], 503)
            ->push(['results' => [['title' => 'Recovered result']]], 200);

        $gateway = new SearchGateway([
            'base_url' => 'http://search.internal',
            'retry_attempts' => 2,
            'retry_delay_ms' => 1,
        ]);

        $result = $gateway->searchWeb('recovery test');

        $this->assertSame('ok', $result['status']);
        $this->assertCount(1, $result['results']);
        Http::assertSentCount(2);
    }

    public function test_search_web_returns_degraded_payload_when_upstream_fails(): void
    {
        Http::fake([
            'search.internal/api/search*' => Http::response(['message' => 'down'], 500),
            'search.internal/api/health/live' => Http::response(['status' => 'down'], 500),
        ]);

        $gateway = new SearchGateway([
            'base_url' => 'http://search.internal',
            'retry_attempts' => 1,
            'retry_delay_ms' => 0,
        ]);

        $result = $gateway->searchWeb('outage test');

        $this->assertSame('degraded', $result['status']);
        $this->assertSame([], $result['results']);
        $this->assertFalse($gateway->isHealthy());
    }
}
