<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class BrowserForumTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test', 'services.forum.default_community_slug' => 'general']);
        Http::preventStrayRequests();
    }

    public function test_guest_cannot_create_but_the_draft_is_retained(): void
    {
        $this->postJson('/community/threads', ['title' => 'Question', 'source_url' => 'https://example.test'])
            ->assertUnauthorized()->assertSessionHas('forum_thread_draft.title', 'Question');
        Http::assertNothingSent();
    }

    public function test_session_creation_uses_the_configured_community_and_ignores_browser_tokens(): void
    {
        Http::fake([
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/threads' => Http::response(['data' => ['id' => 12]], 201),
        ]);
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]])
            ->postJson('/community/threads', ['title' => 'Question', 'source_url' => 'https://example.test', 'community_slug' => 'other'], ['Authorization' => 'Bearer browser-token'])
            ->assertCreated()->assertJsonPath('data.id', 12)->assertSessionMissing('forum_thread_draft');
        Http::assertSent(fn ($request) => $request->url() === 'http://forum.test/api/threads' && $request['community_slug'] === 'general'
            && $request->hasHeader('Authorization', 'Bearer server-token'));
    }

    public function test_discussion_pagination_forwards_the_session_for_private_membership(): void
    {
        Http::fake([
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/threads/by-url*' => Http::response(['data' => [], 'meta' => ['total' => 0]]),
        ]);
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]])
            ->getJson('/community/threads?'.http_build_query(['url' => 'https://example.test/?a=1&b=two', 'page' => 2, 'sort' => 'new']))->assertOk();
        Http::assertSent(fn ($request) => str_contains($request->url(), '/api/threads/by-url?') && $request['url'] === 'https://example.test/?a=1&b=two' && $request['page'] == 2 && $request['sort'] === 'new'
            && $request->hasHeader('Authorization', 'Bearer server-token'));
    }

    public function test_html_validation_keeps_the_source_and_draft_visible(): void
    {
        Http::fake(['forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']])]);
        $source = 'https://example.test/source?a=1&b=two';
        $url = '/discussions?'.http_build_query(['url' => $source]);
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]])
            ->from($url)->post('/community/threads', ['title' => '   ', 'body' => 'Keep this context', 'source_url' => $source])
            ->assertRedirect($url)->assertSessionHasErrors('title');
        $this->get($url)->assertOk()->assertSee('Keep this context')->assertSee('The title field is required.')
            ->assertSee('class="mf-create-thread"  open', false);
    }
}
