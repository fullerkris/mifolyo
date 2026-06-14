<?php

namespace Tests\Feature;

// use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class ExampleTest extends TestCase
{
    public function test_live_health_endpoint_returns_json(): void
    {
        $response = $this->getJson('/api/health/live');

        $response
            ->assertOk()
            ->assertJson(['status' => 'up']);
    }

    public function test_search_returns_json_for_empty_query(): void
    {
        $response = $this->getJson('/api/search');

        $response
            ->assertOk()
            ->assertJsonPath('query', '')
            ->assertJsonPath('results', [])
            ->assertJsonPath('meta.total', 0)
            ->assertJsonPath('meta.source', 'query-engine');
    }
}
