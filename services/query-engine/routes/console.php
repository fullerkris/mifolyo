<?php

use Illuminate\Foundation\Inspiring;
use Illuminate\Support\Facades\Artisan;
use Illuminate\Support\Facades\Redis;
use Symfony\Component\Console\Command\Command;

Artisan::command('inspire', function () {
    $this->comment(Inspiring::quote());
})->purpose('Display an inspiring quote');

Artisan::command('security:purge-legacy-search-terms', function (): int {
    $prefix = config('database.redis.options.prefix');

    if (! is_string($prefix) || $prefix === '') {
        $this->error('Redis key prefix is not configured; no key was deleted.');

        return Command::FAILURE;
    }

    // Pass the logical key to Laravel's configured connection. The client adds
    // the configured prefix exactly once; never construct or scan physical keys.
    $deleted = Redis::connection('default')->del('top_searches');

    if ($deleted === 0) {
        $this->info('Legacy search-term key was already absent.');
    } else {
        $this->info('Legacy search-term key deleted.');
    }

    return Command::SUCCESS;
})->purpose('Delete only the prefixed legacy top_searches key without reading its content');
