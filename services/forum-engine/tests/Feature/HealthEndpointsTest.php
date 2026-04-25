<?php

namespace Tests\Feature;

use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class HealthEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_live_health_endpoint_returns_ok(): void
    {
        $response = $this->getJson('/api/health/live');

        $response
            ->assertOk()
            ->assertJson([
                'status' => 'ok',
                'service' => 'forum-engine',
            ]);
    }

    public function test_ready_health_endpoint_reports_database_check(): void
    {
        $response = $this->getJson('/api/health/ready');

        $response
            ->assertOk()
            ->assertJsonPath('status', 'ready')
            ->assertJsonPath('service', 'forum-engine')
            ->assertJsonPath('checks.database', true);
    }
}
