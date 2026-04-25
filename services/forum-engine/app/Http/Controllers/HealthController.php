<?php

namespace App\Http\Controllers;

use Illuminate\Http\JsonResponse;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Redis;
use Predis\Client;
use Throwable;

class HealthController extends Controller
{
    public function live(): JsonResponse
    {
        return response()->json([
            'status' => 'ok',
            'service' => 'forum-engine',
        ]);
    }

    public function ready(): JsonResponse
    {
        $databaseReady = $this->isDatabaseReady();
        $redisReady = $this->isRedisReady();

        $checks = [
            'database' => $databaseReady,
            'redis' => $redisReady,
        ];

        $isReady = $databaseReady && $redisReady !== false;

        return response()->json([
            'status' => $isReady ? 'ready' : 'not_ready',
            'service' => 'forum-engine',
            'checks' => $checks,
        ], $isReady ? 200 : 503);
    }

    private function isDatabaseReady(): bool
    {
        try {
            DB::select('select 1');

            return true;
        } catch (Throwable) {
            return false;
        }
    }

    private function isRedisReady(): ?bool
    {
        if (! $this->shouldCheckRedis()) {
            return null;
        }

        try {
            Redis::connection()->ping();

            return true;
        } catch (Throwable) {
            return false;
        }
    }

    private function shouldCheckRedis(): bool
    {
        if (app()->environment('testing')) {
            return false;
        }

        $client = (string) config('database.redis.client', '');

        if ($client === 'phpredis') {
            return extension_loaded('redis');
        }

        if ($client === 'predis') {
            return class_exists(Client::class);
        }

        return false;
    }
}
