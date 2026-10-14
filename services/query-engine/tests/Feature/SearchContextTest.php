<?php

namespace Tests\Feature;

use App\Services\ForumApi;
use App\Support\SearchContext;
use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class SearchContextTest extends TestCase
{
    public function test_context_keeps_exact_query_mode_and_page_but_rejects_other_destinations(): void
    {
        $from = '/search/exact?'.http_build_query(['q' => 'C++ & café #1', 'page' => 2], '', '&', PHP_QUERY_RFC3986);
        $this->assertSame($from, SearchContext::url($from.'&unexpected=drop'));
        $this->assertSame($from.'#mf-return', ForumApi::returnTo($from.'#mf-return'));
        $this->assertSame('/', ForumApi::returnTo('//evil.test/search?q=a#mf-return'));
        $this->assertSame('/', ForumApi::returnTo('/threads/1#mf-return'));
        foreach (['//evil.test/search?q=a', 'https://evil.test/search?q=a', '/sign-out', '/search/../sign-out?q=a', '/search?q[]=a', '/search?q=a&page[]=2', '/search?q=a&page=0', '/search?q=a#external', "/search?q=a\r\nX:bad", '/\\evil.test', null, ['bad']] as $input) {
            $this->assertNull(SearchContext::url($input));
        }
        $this->get('/discussions?'.http_build_query(['url' => 'https://example.test', 'from' => $from]))
            ->assertOk()->assertSee('href="'.e($from).'#mf-return"', false);
        $this->get('/discussions?url=https://example.test&from=https://evil.test')->assertOk()->assertDontSee('data-back-to-results', false)->assertSee('Search MiFolyo');
    }

    public function test_context_survives_html_thread_creation_reply_and_reauthentication(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
        Http::fake([
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/threads' => Http::response(['data' => ['id' => 12]], 201),
            'forum.test/api/posts/12/comments' => Http::sequence()->push(['data' => ['id' => 9]], 201)->push([], 401),
            'forum.test/api/auth/login' => Http::response(['data' => ['token' => 'new-token', 'user' => ['id' => 3, 'username' => 'reader']]]),
        ]);
        $from = '/search/exact?q=C%2B%2B%20%26%20caf%C3%A9&page=2';
        $thread = '/threads/12?'.http_build_query(['from' => $from]);
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]])
            ->post('/community/threads', ['title' => 'Question', 'source_url' => 'https://example.test', 'from' => $from])->assertRedirect($thread);
        $this->post('/threads/12/comments', ['body' => 'Perspective', 'from' => $from])->assertRedirect($thread.'#comment-9');
        $this->post('/threads/12/comments', ['body' => 'Saved draft', 'from' => $from])
            ->assertRedirect('/sign-in?'.http_build_query(['return_to' => $thread]))->assertSessionHas('forum_drafts.12.body', 'Saved draft');
        $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'test-password', 'return_to' => $thread])->assertRedirect($thread);
        Http::assertNotSent(fn ($request) => str_contains($request->url(), '/api/posts/') && isset($request['from']));
    }

    public function test_throttled_sign_in_retry_keeps_the_reading_destination(): void
    {
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
        Http::fake(['forum.test/api/auth/login' => Http::response(['message' => 'Invalid credentials'], 422)]);
        $returnTo = '/threads/12?'.http_build_query(['from' => '/search/exact?q=staging&page=2']);
        for ($attempt = 0; $attempt < 10; $attempt++) {
            $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'incorrect', 'return_to' => $returnTo])->assertRedirect();
        }
        $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'incorrect', 'return_to' => $returnTo])
            ->assertStatus(429)->assertHeader('Retry-After')
            ->assertSee('href="'.e(route('account.sign-in', ['return_to' => $returnTo])).'"', false);
        Http::assertSentCount(10);
    }
}
