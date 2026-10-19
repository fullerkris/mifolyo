<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class AccountSettingsTest extends TestCase
{
    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    private function signIn(): void
    {
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-secret'), 'user' => ['id' => 3, 'username' => 'reader']]]);
    }

    private function fakeForum($deletion): void
    {
        Http::fake([
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader', 'email' => 'reader@example.test', 'level' => 4]]),
            'forum.test/api/auth/account' => $deletion,
        ]);
    }

    public function test_settings_require_login_and_display_fresh_account_fields_with_a_safe_return(): void
    {
        $this->get('/account')->assertRedirect('/sign-in?return_to=%2Faccount%3Freturn_to%3D%252F');
        $this->fakeForum(Http::response(['deleted' => true]));
        $this->signIn();
        $this->get('/account?return_to=//evil.test')->assertOk()->assertSee('reader@example.test')->assertSee('Level 4')->assertSee('Delete your account')
            ->assertSee('href="/"', false)->assertDontSee('server-secret')->assertDontSee('evil.test')->assertHeader('Referrer-Policy', 'no-referrer');
        $returnTo = '/threads/12?from=%2Fimages%3Fq%3Dlibrary%26page%3D2&reply_to=4#comment-4';
        $resetUrl = route('account.password.request', ['return_to' => route('account.show', ['return_to' => $returnTo], false)]);
        $this->get('/account?'.http_build_query(['return_to' => $returnTo]))->assertOk()->assertSee('href="'.e($resetUrl).'"', false);
    }

    public function test_confirmed_deletion_uses_server_credentials_and_clears_private_session_state(): void
    {
        $this->fakeForum(Http::response(['deleted' => true]));
        $this->signIn();
        $this->withSession(['forum_drafts' => [12 => ['body' => 'Private draft']], 'password_reset' => ['token' => 'old']]);
        $this->delete('/account', ['password' => 'current-password', 'confirmation' => '1'])->assertRedirect('/')
            ->assertSessionMissing('forum')->assertSessionMissing('forum_drafts')->assertSessionMissing('password_reset');
        Http::assertSent(fn ($request) => $request->method() === 'DELETE' && $request->hasHeader('Authorization', 'Bearer server-secret')
            && $request['password'] === 'current-password' && $request['confirmation'] === '1');
    }

    public function test_invalid_or_uncertain_deletions_keep_the_account_and_never_flash_passwords(): void
    {
        $this->fakeForum(Http::sequence()->push(['errors' => ['password' => ['wrong']]], 422)->push(['deleted' => false])->pushFailedConnection());
        $this->signIn();
        foreach (range(1, 3) as $_) {
            $this->delete('/account', ['password' => 'private-password', 'confirmation' => '1'])->assertRedirect('/account?return_to=%2F')->assertSessionHasErrors('account')->assertSessionHas('forum');
            $this->assertArrayNotHasKey('password', session('_old_input', []));
        }
    }

    public function test_deletion_requires_confirmation_and_real_csrf(): void
    {
        $this->fakeForum(Http::response(['deleted' => true]));
        $this->signIn();
        $this->delete('/account', ['password' => 'secret'])->assertSessionHasErrors('confirmation');
        Http::assertNotSent(fn ($request) => $request->method() === 'DELETE');
        $this->app['env'] = 'local';
        $this->delete('/account', ['password' => 'secret', 'confirmation' => '1'])->assertStatus(419)->assertSee('Return to form');
        Http::assertNotSent(fn ($request) => $request->method() === 'DELETE');
    }

    public function test_expiry_and_service_outage_have_recoverable_account_pages(): void
    {
        Http::fake(['forum.test/api/auth/me' => Http::sequence()->push(['message' => 'Expired'], 401)->pushFailedConnection()->pushFailedConnection()]);
        $this->signIn();
        $this->get('/account')->assertRedirect()->assertSessionMissing('forum');
        $this->signIn();
        $this->get('/account')->assertStatus(503)->assertSee('Account service unavailable')->assertSee('Return to form')->assertSessionHas('forum');
    }
}
