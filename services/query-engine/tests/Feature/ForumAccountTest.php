<?php

namespace Tests\Feature;

use App\Services\ForumApi;
use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class ForumAccountTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    public function test_sign_in_stores_an_encrypted_server_token_and_returns_to_the_conversation(): void
    {
        Http::fake(['forum.test/api/auth/login' => Http::response(['data' => [
            'token' => 'private-test-token', 'user' => ['id' => 3, 'username' => 'reader', 'email' => 'reader@example.test'],
        ]])]);
        $response = $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'test-password', 'return_to' => '/threads/12?reply_to=4']);
        $response->assertRedirect('/threads/12?reply_to=4')->assertSessionHas('forum.user', ['id' => 3, 'username' => 'reader']);
        $this->assertSame('private-test-token', Crypt::decryptString(session('forum.token')));
        $this->assertNotSame('private-test-token', session('forum.token'));
        $this->assertArrayNotHasKey('password', session('_old_input', []));
        $response->assertDontSee('private-test-token');
        Http::assertSent(fn ($request) => $request['email'] === 'reader@example.test' && $request['password'] === 'test-password');
    }

    public function test_registration_forwards_confirmation_and_reports_duplicate_details(): void
    {
        Http::fake(['forum.test/api/auth/register' => Http::response(['errors' => ['email' => ['Email already registered.']]], 422)]);
        $this->post('/register', [
            'name' => 'Reader', 'username' => 'reader', 'email' => 'reader@example.test',
            'password' => 'test-password', 'password_confirmation' => 'test-password', 'return_to' => '/threads/12',
        ])->assertRedirect('/register?return_to=%2Fthreads%2F12')->assertSessionHasErrors('email')->assertSessionMissing('forum');
        Http::assertSent(fn ($request) => $request['password_confirmation'] === 'test-password');
        $this->assertArrayNotHasKey('password_confirmation', session('_old_input', []));
    }

    public function test_credentials_errors_and_outages_preserve_only_safe_fields(): void
    {
        Http::fake(['forum.test/api/auth/login' => Http::sequence()->push(['message' => 'Invalid credentials.'], 422)->pushFailedConnection()]);
        $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'wrong', 'return_to' => '/search?q=alpha'])
            ->assertSessionHasErrors('account')->assertSessionMissing('forum');
        $this->assertSame(['email' => 'reader@example.test'], session('_old_input'));
        $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'test-password'])
            ->assertSessionHasErrors('account')->assertSessionMissing('forum');
        $this->get('/sign-in')->assertOk()->assertSee('temporarily unavailable')->assertDontSee('test-password');
    }

    public function test_external_or_action_return_targets_are_rejected(): void
    {
        foreach (['https://evil.test', '//evil.test', '/\\evil.test', "/search\r\nX-Test: bad", '/sign-out', '/sign-in', '/threads/1/../../sign-out', ['bad']] as $path) {
            $this->assertSame('/', ForumApi::returnTo($path));
        }
        $this->assertSame('/discussions?url=https%3A%2F%2Fexample.test', ForumApi::returnTo('/discussions?url=https%3A%2F%2Fexample.test'));
    }

    public function test_expired_sessions_become_guest_views_without_leaking_tokens(): void
    {
        Http::fake(['forum.test/api/auth/me' => Http::response(['message' => 'Token expired.'], 401)]);
        $this->withSession(['forum' => ['token' => Crypt::encryptString('expired-token'), 'user' => ['id' => 3, 'username' => 'reader']]])
            ->get('/')->assertOk()->assertSee('Sign in')->assertDontSee('Sign out')->assertDontSee('expired-token')->assertSessionMissing('forum');
    }

    public function test_sign_out_revokes_the_token_and_clears_drafts_even_during_an_outage(): void
    {
        Http::fake([
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/auth/logout' => Http::failedConnection(),
        ]);
        $this->withSession([
            'forum' => ['token' => Crypt::encryptString('test-token'), 'user' => ['id' => 3, 'username' => 'reader']],
            'forum_drafts' => [12 => ['body' => 'private draft']],
        ])->post('/sign-out')->assertRedirect('/')->assertSessionMissing('forum')->assertSessionMissing('forum_drafts');
        Http::assertSent(fn ($request) => $request->url() === 'http://forum.test/api/auth/logout' && $request->hasHeader('Authorization', 'Bearer test-token'));
    }

    public function test_browser_writes_require_csrf_even_with_a_valid_forum_session(): void
    {
        // Laravel normally bypasses CSRF in unit tests; exercise the real web middleware.
        $this->app['env'] = 'local';
        $this->post('/sign-in', ['email' => 'reader@example.test', 'password' => 'test-password'])->assertStatus(419);
        $this->postJson('/community/threads', ['title' => 'Test', 'source_url' => 'https://example.test'])->assertStatus(419);
        $this->post('/threads/12/comments', ['body' => 'Test'])->assertStatus(419);
        Http::assertNothingSent();
    }
}
