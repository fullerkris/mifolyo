<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class ForumThreadsProxyTest extends TestCase
{
    public function test_threads_by_url_proxy_forwards_to_forum_service(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);

        Http::fake([
            'forum.test/api/threads/by-url*' => Http::response([
                'data' => [
                    ['id' => 12, 'title' => 'Useful source'],
                ],
                'meta' => ['total' => 1],
            ]),
        ]);

        $response = $this->getJson('/api/threads/by-url?url='.urlencode('https://example.com/source'));

        $response
            ->assertOk()
            ->assertJsonPath('data.0.id', 12)
            ->assertJsonPath('meta.total', 1);

        Http::assertSent(fn ($request) => $request->url() === 'http://forum.test/api/threads/by-url?url=https%3A%2F%2Fexample.com%2Fsource&sort=top&per_page=20&page=1');
    }

    public function test_threads_proxy_preserves_page_sort_and_source_query(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::fake(['forum.test/*' => Http::response(['data' => [], 'meta' => ['current_page' => 3]])]);
        $url = 'https://example.com/source?a=1&b=two+words';

        $this->getJson('/api/threads/by-url?'.http_build_query(['url' => $url, 'sort' => 'new', 'page' => 3]))
            ->assertOk()->assertJsonPath('meta.current_page', 3);

        Http::assertSent(fn ($request) => $request['url'] === $url && $request['sort'] === 'new' && $request['page'] === '3');
        $this->getJson('/api/threads/by-url?'.http_build_query(['url' => $url, 'page' => 0]))->assertUnprocessable();
        $this->getJson('/api/threads/by-url?'.http_build_query(['url' => $url, 'page' => 10001]))->assertUnprocessable();
    }

    public function test_threads_by_url_proxy_returns_degraded_response_when_forum_is_unavailable(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);

        Http::fake(function (): never {
            throw new \RuntimeException('Connection failed');
        });

        $this->getJson('/api/threads/by-url?url='.urlencode('https://example.com/source'))
            ->assertStatus(503)
            ->assertJsonPath('message', 'Forum threads are unavailable right now.')
            ->assertJsonPath('data', []);
    }

    public function test_create_thread_proxy_forwards_payload_and_bearer_token_to_forum_service(): void
    {
        config([
            'services.forum.base_url' => 'http://forum.test',
            'services.forum.default_community_slug' => 'general',
        ]);

        Http::fake([
            'forum.test/api/threads' => Http::response([
                'data' => [
                    'id' => 42,
                    'title' => 'Worth discussing',
                    'source_url' => 'https://example.com/source',
                ],
            ], 201),
        ]);

        $response = $this->withHeader('Authorization', 'Bearer test-token')->postJson('/api/threads', [
            'title' => 'Worth discussing',
            'body' => 'This source has a strong bibliography.',
            'source_url' => 'https://example.com/source',
        ]);

        $response
            ->assertCreated()
            ->assertJsonPath('data.id', 42);

        Http::assertSent(function ($request): bool {
            return $request->url() === 'http://forum.test/api/threads'
                && $request->hasHeader('Authorization', 'Bearer test-token')
                && $request['community_slug'] === 'general'
                && $request['title'] === 'Worth discussing'
                && $request['body'] === 'This source has a strong bibliography.'
                && $request['source_url'] === 'https://example.com/source';
        });
    }

    public function test_create_thread_proxy_returns_degraded_response_when_forum_is_unavailable(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);

        Http::fake(function (): never {
            throw new \RuntimeException('Connection failed');
        });

        $this->postJson('/api/threads', [
            'title' => 'Worth discussing',
            'source_url' => 'https://example.com/source',
        ])
            ->assertStatus(503)
            ->assertJsonPath('message', 'Forum threads are unavailable right now.')
            ->assertJsonPath('data', null);
    }
}
