<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class PasswordRecoveryTest extends TestCase
{
    private string $token;

    protected function setUp(): void
    {
        parent::setUp();
        $this->token = str_repeat('a', 64);
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    private function openLink(string $return = '/threads/12?reply_to=4#comment-4'): string
    {
        $this->get('/reset-password?'.http_build_query(['email' => 'reader@example.test', 'token' => $this->token, 'return_to' => $return]))
            ->assertRedirect('/reset-password')->assertHeader('Referrer-Policy', 'no-referrer');

        return session('password_reset.id');
    }

    public function test_email_requests_preserve_exact_safe_reading_context_and_show_generic_success(): void
    {
        Http::fake(['forum.test/api/auth/forgot-password' => Http::response(['message' => 'generic'], 202)]);
        $return = '/threads/12?from=%2Fimages%3Fq%3Dlibrary%26page%3D2&reply_to=4#comment-4';
        $this->post('/forgot-password', ['email' => 'reader@example.test', 'return_to' => $return])->assertRedirect('/forgot-password?'.http_build_query(['return_to' => $return]));
        $this->get('/forgot-password?'.http_build_query(['return_to' => $return]))->assertOk()->assertSee('If an account matches that email')->assertSee('Back to sign in');
        Http::assertSent(fn ($request) => $request['email'] === 'reader@example.test' && $request['return_to'] === $return);
        $this->post('/forgot-password', ['email' => 'unknown@example.test', 'return_to' => '//evil.test'])->assertRedirect('/forgot-password?return_to=%2F');
        Http::assertSent(fn ($request) => $request['email'] === 'unknown@example.test' && $request['return_to'] === '/');
    }

    public function test_reset_secrets_stay_server_side_and_success_returns_to_sign_in_without_replaying_actions(): void
    {
        Http::fake(['forum.test/api/auth/reset-password' => Http::response(['reset' => true, 'message' => 'Reset'])]);
        $id = $this->openLink();
        $this->assertSame($this->token, Crypt::decryptString(session('password_reset.token')));
        $this->get('/reset-password')->assertOk()->assertSee('reader@example.test')->assertDontSee($this->token)->assertDontSee('name="token"', false);
        $this->withSession(['forum_drafts' => [12 => ['body' => 'Retain my draft']]]);
        $this->post('/reset-password', ['reset_id' => $id, 'password' => 'new-password-123', 'password_confirmation' => 'new-password-123'])
            ->assertRedirect('/sign-in?return_to=%2Fthreads%2F12%3Freply_to%3D4%23comment-4')->assertSessionMissing('password_reset')->assertSessionMissing('forum')
            ->assertSessionHas('forum_drafts.12.body', 'Retain my draft');
        Http::assertSent(fn ($request) => $request['token'] === $this->token && $request['email'] === 'reader@example.test');
        $this->assertArrayNotHasKey('password', session('_old_input', []));
        Http::assertSentCount(1);
    }

    public function test_validation_and_stale_tabs_cannot_submit_another_reset_flow(): void
    {
        $first = $this->openLink();
        $second = $this->openLink();
        $this->assertNotSame($first, $second);
        $this->post('/reset-password', ['reset_id' => $first, 'password' => 'new-password-123', 'password_confirmation' => 'new-password-123'])->assertSessionHasErrors('token');
        $this->post('/reset-password', ['reset_id' => $second, 'password' => 'new-password-123', 'password_confirmation' => 'different'])->assertSessionHasErrors('password');
        $this->assertArrayNotHasKey('password', session('_old_input', []));
        $this->assertArrayNotHasKey('password_confirmation', session('_old_input', []));
        Http::assertNothingSent();
        $this->get('/reset-password?token=bad&email=reader@example.test')->assertStatus(422)->assertSee('Request a new reset link')->assertDontSee('name="password"', false);
        $this->get('/reset-password?token='.$this->token.'&email=invalid')->assertStatus(422)->assertDontSee($this->token);
    }

    public function test_expired_tokens_and_outages_offer_safe_recovery_without_credential_leaks(): void
    {
        Http::fake([
            'forum.test/api/auth/forgot-password' => Http::failedConnection(),
            'forum.test/api/auth/reset-password' => Http::sequence()->push(['errors' => ['token' => ['internal secret']]], 422)->pushFailedConnection()->push(['reset' => false]),
        ]);
        $this->post('/forgot-password', ['email' => 'reader@example.test'])->assertSessionHasErrors('account');
        $id = $this->openLink();
        $input = ['reset_id' => $id, 'password' => 'new-password-123', 'password_confirmation' => 'new-password-123'];
        $this->post('/reset-password', $input)->assertRedirect('/reset-password')->assertSessionHasErrors('token');
        $this->get('/reset-password')->assertOk()->assertSee('invalid or has expired')->assertDontSee('internal secret')->assertDontSee($this->token);
        $this->post('/reset-password', $input)->assertSessionHasErrors('account');
        $this->get('/reset-password')->assertSee('Could not confirm')->assertDontSee('new-password-123');
        $this->post('/reset-password', $input)->assertSessionHasErrors('account')->assertSessionHas('password_reset');
    }

    public function test_recovery_writes_require_csrf_and_throttle_retry_preserves_destination(): void
    {
        $this->app['env'] = 'local';
        $this->post('/forgot-password', ['email' => 'reader@example.test'])->assertStatus(419);
        $this->post('/reset-password', ['token' => $this->token])->assertStatus(419)->assertDontSee($this->token);
        Http::assertNothingSent();
        $this->app['env'] = 'testing';
        Http::fake(['forum.test/api/auth/forgot-password' => Http::response([], 202)]);
        for ($i = 0; $i < 5; $i++) {
            $this->post('/forgot-password', ['email' => 'reader@example.test'])->assertRedirect();
        }
        $this->post('/forgot-password', ['email' => 'reader@example.test', 'return_to' => '/images?q=library&page=2'])
            ->assertStatus(429)->assertHeader('Retry-After')->assertSee('return_to=%2Fimages%3Fq%3Dlibrary%26page%3D2', false);
    }
}
