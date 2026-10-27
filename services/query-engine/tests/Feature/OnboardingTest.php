<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Route;
use Tests\TestCase;

class OnboardingTest extends TestCase
{
    private array $state = ['welcome' => true, 'discuss' => true, 'vote' => true];

    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    private function forum(array $overrides = []): void
    {
        Http::fake($overrides + [
            'forum.test/api/auth/me' => fn () => Http::response(['data' => ['id' => 3, 'username' => 'reader', 'onboarding' => $this->state]]),
            'forum.test/api/auth/onboarding' => function ($request) {
                $this->state[$request['step']] = false;

                return Http::response(['data' => $this->state]);
            },
            'forum.test/api/posts/12' => Http::response(['data' => ['id' => 12, 'title' => 'A discussion', 'score' => 2, 'source_url' => 'https://example.test']]),
            'forum.test/api/posts/12/comments' => Http::response(['data' => []]),
        ]);
        $this->withSession(['forum' => ['user' => ['id' => 3, 'username' => 'reader'], 'token' => Crypt::encryptString('server-token')]]);
    }

    private function search(?array $results = null): void
    {
        Route::middleware('web')->get('/search', fn () => view('search-results', [
            'query' => 'alpha', 'suggestions' => false, 'total' => 2, 'page' => 1, 'topImages' => [],
            'results' => $results ?? [['_id' => 'https://example.test/one', 'title' => 'First'], ['_id' => 'https://example.test/two', 'title' => 'Second']],
        ]))->name('search');
    }

    public function test_welcome_dismissal_is_confirmed_and_preserves_the_reading_destination(): void
    {
        $this->forum();
        $this->get('/')->assertOk()->assertSee('Welcome to MiFolyo.')->assertSee('data-onboarding-dismiss', false)->assertDontSee('server-token');
        $this->post('/onboarding', ['step' => 'welcome', 'user_id' => 3, 'return_to' => '/threads/12?reply_to=4#comment-4'])
            ->assertRedirect('/threads/12?reply_to=4#comment-4');
        $this->get('/')->assertOk()->assertDontSee('data-onboarding-step="welcome"', false);
        Http::assertSent(fn ($request) => $request->url() === 'http://forum.test/api/auth/onboarding' && $request->data() === ['step' => 'welcome']
            && $request->hasHeader('Authorization', 'Bearer server-token'));
    }

    public function test_first_successful_search_completes_welcome_and_renders_only_one_discuss_hint(): void
    {
        $this->forum();
        $this->search();
        $response = $this->get('/search?q=alpha')->assertOk()->assertDontSee('data-onboarding-step="welcome"', false)
            ->assertSee('aria-describedby="onboarding-discuss"', false);
        $this->assertSame(1, substr_count($response->getContent(), 'data-onboarding-step="discuss"'));
        $this->assertFalse($this->state['welcome']);
        $this->assertTrue($this->state['discuss']);
        $this->get('/')->assertDontSee('Welcome to MiFolyo.');
    }

    public function test_empty_invalid_failed_and_json_searches_do_not_complete_welcome(): void
    {
        $this->forum();
        $this->get('/search?q[]=bad')->assertStatus(422);
        $this->get('/search/exact?q=alpha&page=0')->assertStatus(422);
        $this->search();
        $this->get('/search')->assertOk();
        $this->getJson('/search?q=alpha')->assertOk();
        Route::middleware('web')->get('/search', fn () => abort(503))->name('search');
        $this->get('/search?q=alpha')->assertStatus(503);
        $this->assertTrue($this->state['welcome']);
        Http::assertNotSent(fn ($request) => $request->method() === 'POST');
    }

    public function test_discuss_guidance_attaches_to_the_first_usable_source(): void
    {
        $this->forum();
        $this->search([['_id' => 'javascript:bad()', 'title' => 'Invalid'], ['_id' => 'https://example.test/two', 'title' => 'Second']]);
        $response = $this->get('/search?q=alpha')->assertOk();
        $this->assertSame(1, substr_count($response->getContent(), 'data-onboarding-step="discuss"'));
        $this->assertMatchesRegularExpression('/data-discuss-title="Second"\s+aria-describedby="onboarding-discuss"/', $response->getContent());
    }

    public function test_discussion_pages_complete_discuss_without_javascript_and_vote_hint_uses_account_state(): void
    {
        $this->forum();
        $this->get('/discussions?url=https%3A%2F%2Fexample.test')->assertOk();
        $this->assertFalse($this->state['discuss']);
        $this->get('/threads/12')->assertOk()->assertSee('data-onboarding-step="vote"', false)->assertSee('Found a useful perspective?');
        $this->state['vote'] = false;
        $this->get('/threads/12')->assertOk()->assertDontSee('data-onboarding-step="vote"', false);
    }

    public function test_failed_or_malformed_saves_leave_progress_retryable_and_do_not_break_search(): void
    {
        $this->forum(['forum.test/api/auth/onboarding' => Http::sequence()->pushFailedConnection()->push(['data' => ['welcome' => false]])->pushFailedConnection()]);
        $input = ['step' => 'welcome', 'user_id' => 3, 'return_to' => '/'];
        $this->post('/onboarding', $input)->assertRedirect('/')->assertSessionHasErrors('onboarding');
        $this->get('/')->assertSee('Welcome to MiFolyo.')->assertSee('Please try again.');
        $this->postJson('/onboarding', $input)->assertStatus(503);
        $this->search();
        $this->get('/search?q=alpha')->assertOk()->assertSee('First');
        $this->assertTrue($this->state['welcome']);
    }

    public function test_guest_expired_and_unavailable_accounts_do_not_show_guidance(): void
    {
        $this->get('/')->assertOk()->assertDontSee('Welcome to MiFolyo.');
        $this->postJson('/onboarding', ['step' => 'welcome', 'user_id' => 3])->assertUnauthorized();
        $this->forum(['forum.test/api/auth/me' => Http::sequence()->pushFailedConnection()->push(['data' => ['id' => 3, 'username' => 'reader', 'onboarding' => ['welcome' => 'true']]])->push([], 401)]);
        $this->get('/')->assertOk()->assertDontSee('Welcome to MiFolyo.');
        $this->get('/')->assertOk()->assertDontSee('Welcome to MiFolyo.');
        $this->get('/')->assertOk()->assertDontSee('Welcome to MiFolyo.')->assertSessionMissing('forum');
    }

    public function test_events_validate_account_and_csrf_and_cannot_fake_a_vote_or_external_redirect(): void
    {
        $this->forum();
        $this->postJson('/onboarding', ['step' => 'vote', 'user_id' => 3])->assertUnprocessable();
        $this->postJson('/onboarding', ['step' => 'welcome', 'user_id' => 4])->assertStatus(409);
        Http::assertNotSent(fn ($request) => $request->method() === 'POST');
        $this->post('/onboarding', ['step' => 'welcome', 'user_id' => 3, 'return_to' => 'https://evil.test'])->assertRedirect('/');
        $this->app['env'] = 'local';
        $this->postJson('/onboarding', ['step' => 'discuss', 'user_id' => 3])->assertStatus(419);
        $this->assertTrue($this->state['discuss']);
    }

    public function test_refresh_returns_only_fresh_private_account_state(): void
    {
        $this->forum();
        $response = $this->getJson('/onboarding')->assertOk()->assertExactJson(['user_id' => 3, 'data' => $this->state]);
        $this->assertStringContainsString('no-store', $response->headers->get('Cache-Control'));
        $this->postJson('/onboarding', ['step' => 'discuss', 'user_id' => 3])->assertOk()->assertJsonPath('data.discuss', false);
        $this->getJson('/onboarding')->assertOk()->assertJsonPath('data.discuss', false);
    }

    public function test_expired_html_dismissal_returns_to_a_readable_page(): void
    {
        $this->forum();
        $this->app['env'] = 'local';
        $this->post('/onboarding', ['step' => 'welcome', 'user_id' => 3, 'return_to' => '/threads/12?reply_to=4'])
            ->assertStatus(419)->assertSee('href="/threads/12?reply_to=4"', false)->assertSee('Return to page');
        $this->assertTrue($this->state['welcome']);
    }
}
