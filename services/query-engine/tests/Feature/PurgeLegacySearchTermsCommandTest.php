<?php

namespace Tests\Feature;

use Illuminate\Redis\Connections\Connection;
use Illuminate\Support\Facades\Artisan;
use Illuminate\Support\Facades\Redis;
use Mockery;
use Tests\TestCase;

class PurgeLegacySearchTermsCommandTest extends TestCase
{
    public function test_it_deletes_only_the_logical_prefixed_key_and_is_idempotent(): void
    {
        config()->set('database.redis.options.prefix', 'mifolyo_release_');

        $connection = Mockery::mock(Connection::class);
        $connection->shouldReceive('del')
            ->twice()
            ->with('top_searches')
            ->andReturn(1, 0);
        $connection->shouldNotReceive('get');
        $connection->shouldNotReceive('flushdb');
        $connection->shouldNotReceive('flushall');

        Redis::shouldReceive('connection')
            ->twice()
            ->with('default')
            ->andReturn($connection);

        $this->assertSame(0, Artisan::call('security:purge-legacy-search-terms'));
        $this->assertSame("Legacy search-term key deleted.\n", Artisan::output());

        $this->assertSame(0, Artisan::call('security:purge-legacy-search-terms'));
        $this->assertSame("Legacy search-term key was already absent.\n", Artisan::output());
    }

    public function test_it_fails_closed_when_the_redis_prefix_is_empty(): void
    {
        config()->set('database.redis.options.prefix', '');

        Redis::shouldReceive('connection')->never();

        $this->assertSame(1, Artisan::call('security:purge-legacy-search-terms'));
        $this->assertSame(
            "Redis key prefix is not configured; no key was deleted.\n",
            Artisan::output(),
        );
    }
}
