<?php

namespace App\Services;

use Illuminate\Http\Client\Response;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;
use Throwable;

class SearchGateway
{
    private string $baseUrl;

    private float $timeoutSeconds;

    private float $connectTimeoutSeconds;

    private int $retryAttempts;

    private int $retryDelayMs;

    public function __construct(?array $config = null)
    {
        $settings = $config ?? config('services.search', []);

        $this->baseUrl = (string) ($settings['base_url'] ?? 'http://localhost:8080');
        $this->timeoutSeconds = (float) ($settings['timeout_seconds'] ?? 3);
        $this->connectTimeoutSeconds = (float) ($settings['connect_timeout_seconds'] ?? 1.5);
        $this->retryAttempts = max(1, (int) ($settings['retry_attempts'] ?? 2));
        $this->retryDelayMs = max(0, (int) ($settings['retry_delay_ms'] ?? 150));
    }

    public function searchWeb(string $query, int $page = 1, int $perPage = 10): array
    {
        $response = $this->sendRequest('/api/search', [
            'q' => $query,
            'page' => max(1, $page),
            'per_page' => min(max(1, $perPage), 100),
        ]);

        if (! $response) {
            return [
                'status' => 'degraded',
                'query' => $query,
                'results' => [],
                'meta' => [
                    'source' => 'search-service',
                    'error' => 'search_unavailable',
                ],
            ];
        }

        $payload = $response->json();

        return [
            'status' => 'ok',
            'query' => $query,
            'results' => $payload['results'] ?? $payload['data'] ?? [],
            'meta' => $payload['meta'] ?? [
                'source' => 'search-service',
            ],
        ];
    }

    public function isHealthy(): bool
    {
        $response = $this->sendRequest('/api/health/live');

        return $response ? $response->successful() : false;
    }

    private function sendRequest(string $path, array $query = []): ?Response
    {
        try {
            $response = Http::baseUrl($this->baseUrl)
                ->acceptJson()
                ->connectTimeout($this->connectTimeoutSeconds)
                ->timeout($this->timeoutSeconds)
                ->retry($this->retryAttempts, $this->retryDelayMs, throw: false)
                ->get($path, $query);

            if ($response->successful()) {
                return $response;
            }

            Log::warning('search_gateway.request_failed', [
                'path' => $path,
                'status' => $response->status(),
                'response_body' => $response->body(),
            ]);

            return null;
        } catch (Throwable $exception) {
            Log::warning('search_gateway.exception', [
                'path' => $path,
                'message' => $exception->getMessage(),
                'exception' => $exception::class,
            ]);

            return null;
        }
    }
}
