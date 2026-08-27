<?php

namespace Tests\Feature;

use App\Http\Middleware\StoreSearchTerm;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Redis;
use Tests\TestCase;

class ReleaseSecurityTest extends TestCase
{
    public function test_page_connections_rejects_untrusted_url_shapes_before_database_access(): void
    {
        $this->getJson('/api/page-connections')->assertUnprocessable();
        $this->getJson('/api/page-connections?url=ftp%3A%2F%2Fexample.test%2F')->assertUnprocessable();
        $this->getJson('/api/page-connections?url[]=https%3A%2F%2Fexample.test%2F')->assertUnprocessable();
        $this->getJson('/api/page-connections?'.http_build_query([
            'url' => 'https://example.test/'.str_repeat('a', 2049),
        ]))->assertUnprocessable();
    }

    public function test_page_connections_route_is_throttled(): void
    {
        $route = app('router')->getRoutes()->match(
            Request::create('/api/page-connections?url=https%3A%2F%2Fexample.test%2F', 'GET'),
        );

        $this->assertContains('throttle:20,1', $route->gatherMiddleware());
    }

    public function test_image_search_validates_consumed_suggestions_input(): void
    {
        $this->getJson('/api/search_images?suggestions=not-a-boolean')->assertUnprocessable();
        $this->getJson('/api/search_images?suggestions[]=1')->assertUnprocessable();
        $this->get('/api/search_images?suggestions=1')
            ->assertOk()
            ->assertViewHas('suggestions', true);
    }

    public function test_search_telemetry_retains_only_the_aggregate_count(): void
    {
        Redis::shouldReceive('incr')->once()->with('total_searches')->andReturn(1);
        Redis::shouldReceive('zincrby')->never();
        Redis::shouldReceive('zremrangebyrank')->never();
        Redis::shouldReceive('expire')->once()->with('total_searches', 86400)->andReturn(true);

        $request = Request::create('/api/search', 'GET');
        $request->attributes->set('processedQuery', 'private search text');

        $response = app(StoreSearchTerm::class)->handle(
            $request,
            static fn () => response('ok'),
        );

        $this->assertSame(200, $response->getStatusCode());
    }

    public function test_legacy_top_search_endpoints_never_read_or_expose_terms(): void
    {
        Redis::shouldReceive('zrevrange')->never();

        $this->getJson('/api/get_top_searches')
            ->assertOk()
            ->assertExactJson(['searches' => []]);
        $this->getJson('/api/get_search_suggestions?q=previously-stored-secret')
            ->assertOk()
            ->assertExactJson(['searches' => []]);
    }
}
