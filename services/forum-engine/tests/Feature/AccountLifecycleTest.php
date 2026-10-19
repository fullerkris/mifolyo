<?php

namespace Tests\Feature;

use App\Http\Controllers\AuthController;
use App\Models\Comment;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use App\Notifications\SearchUiResetPassword;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Notification;
use Illuminate\Support\Facades\Password;
use Illuminate\Support\Str;
use Symfony\Component\HttpKernel\Exception\HttpException;
use Symfony\Component\Mailer\Exception\TransportException;
use Tests\TestCase;

class AccountLifecycleTest extends TestCase
{
    use RefreshDatabase;

    protected function setUp(): void
    {
        parent::setUp();
        config(['services.search_ui.url' => 'https://search.example.test']);
    }

    private function headers(User $user): array
    {
        $token = Str::random(60);
        $user->forceFill(['api_token' => hash('sha256', $token), 'api_token_expires_at' => now()->addHour()])->save();
        app('auth')->forgetGuards();

        return ['Authorization' => 'Bearer '.$token];
    }

    public function test_reset_mail_uses_configured_origin_and_keeps_unknown_and_throttled_accounts_private(): void
    {
        Notification::fake();
        $user = User::factory()->create();
        $return = '/threads/12?from=%2Fimages%3Fq%3Dlibrary%26page%3D2&reply_to=4#comment-4';
        $response = $this->postJson('/api/auth/forgot-password', ['email' => $user->email, 'return_to' => $return], ['Host' => 'untrusted.test'])->assertStatus(202);
        $this->postJson('/api/auth/forgot-password', ['email' => 'unknown@example.test'])->assertStatus(202)->assertExactJson($response->json());
        $this->postJson('/api/auth/forgot-password', ['email' => $user->email])->assertStatus(202)->assertExactJson($response->json());
        Notification::assertSentToTimes($user, SearchUiResetPassword::class, 1);
        Notification::assertSentTo($user, SearchUiResetPassword::class, function ($notification) use ($user, $return, $response) {
            $url = $notification->toMail($user)->actionUrl;
            $this->assertSame('search.example.test', parse_url($url, PHP_URL_HOST));
            parse_str(parse_url($url, PHP_URL_QUERY), $query);
            $this->assertSame($return, $query['return_to']);
            $this->assertSame($user->email, $query['email']);
            $this->assertTrue(Password::tokenExists($user, $query['token']));
            $response->assertDontSee($query['token']);

            return true;
        });
    }

    public function test_reset_links_reject_external_returns_and_missing_public_origin(): void
    {
        Notification::fake();
        $user = User::factory()->create();
        $this->postJson('/api/auth/forgot-password', ['email' => $user->email, 'return_to' => '//evil.test'])->assertStatus(202);
        Notification::assertSentTo($user, SearchUiResetPassword::class, function ($notification) use ($user) {
            parse_str(parse_url($notification->toMail($user)->actionUrl, PHP_URL_QUERY), $query);
            $this->assertSame('/', $query['return_to']);

            return true;
        });
        config(['services.search_ui.url' => null]);
        $this->postJson('/api/auth/forgot-password', ['email' => 'unknown@example.test'])->assertStatus(503);
    }

    public function test_failed_mail_delivery_does_not_throttle_the_next_delivery_attempt(): void
    {
        $user = User::factory()->create();
        Notification::shouldReceive('send')->once()->andThrow(new TransportException('offline'));
        $this->postJson('/api/auth/forgot-password', ['email' => $user->email])->assertStatus(503);
        $this->assertDatabaseMissing('password_reset_tokens', ['email' => $user->email]);
        Notification::fake();
        $this->postJson('/api/auth/forgot-password', ['email' => $user->email])->assertStatus(202);
        Notification::assertSentTo($user, SearchUiResetPassword::class);
    }

    public function test_an_in_flight_refresh_cannot_reissue_a_revoked_token(): void
    {
        $user = User::factory()->create();
        $headers = $this->headers($user);
        $stale = $user->fresh();
        $request = Request::create('/api/auth/refresh', 'POST', server: ['HTTP_AUTHORIZATION' => $headers['Authorization']]);
        $request->setUserResolver(fn () => $stale);
        $user->forceFill(['api_token' => null])->save();
        try {
            app(AuthController::class)->refresh($request);
            $this->fail('A revoked token must not be refreshed from a stale guard user.');
        } catch (HttpException $exception) {
            $this->assertSame(401, $exception->getStatusCode());
        }
        $this->assertNull($user->fresh()->api_token);
    }

    public function test_an_in_flight_logout_cannot_clear_a_new_login_token(): void
    {
        $user = User::factory()->create();
        $headers = $this->headers($user);
        $stale = $user->fresh();
        $request = Request::create('/api/auth/logout', 'POST', server: ['HTTP_AUTHORIZATION' => $headers['Authorization']]);
        $request->setUserResolver(fn () => $stale);
        $this->headers($user);
        $newToken = $user->fresh()->api_token;
        app(AuthController::class)->logout($request);
        $this->assertSame($newToken, $user->fresh()->api_token);
    }

    public function test_reset_consumes_the_token_and_revokes_api_and_database_sessions(): void
    {
        $user = User::factory()->create();
        $headers = $this->headers($user);
        $other = User::factory()->create();
        foreach ([$user, $other] as $member) {
            DB::table('sessions')->insert(['id' => 'session-'.$member->id, 'user_id' => $member->id, 'payload' => '', 'last_activity' => time()]);
        }
        $input = ['email' => $user->email, 'token' => Password::createToken($user), 'password' => 'new-password-123', 'password_confirmation' => 'new-password-123'];
        $this->postJson('/api/auth/reset-password', $input)->assertOk();
        $this->assertTrue(Hash::check($input['password'], $user->fresh()->password));
        $this->assertNull($user->fresh()->api_token);
        $this->assertFalse(Password::tokenExists($user, $input['token']));
        $this->assertDatabaseMissing('sessions', ['user_id' => $user->id]);
        $this->assertDatabaseHas('sessions', ['user_id' => $other->id]);
        app('auth')->forgetGuards();
        $this->getJson('/api/auth/me', $headers)->assertUnauthorized();
        $this->postJson('/api/auth/reset-password', $input)->assertUnprocessable();
        $this->postJson('/api/auth/login', ['email' => $user->email, 'password' => 'password'])->assertUnprocessable();
        $this->postJson('/api/auth/login', ['email' => $user->email, 'password' => $input['password']])->assertOk();
    }

    public function test_invalid_expired_and_wrong_email_tokens_do_not_change_passwords(): void
    {
        $user = User::factory()->create();
        $other = User::factory()->create();
        $token = Password::createToken($user);
        $input = ['email' => $user->email, 'token' => $token, 'password' => 'new-password-123', 'password_confirmation' => 'new-password-123'];
        $this->postJson('/api/auth/reset-password', array_replace($input, ['email' => $other->email]))->assertUnprocessable();
        $this->postJson('/api/auth/reset-password', array_replace($input, ['token' => str_repeat('0', 64)]))->assertUnprocessable();
        $this->postJson('/api/auth/reset-password', array_replace($input, ['password_confirmation' => 'different']))->assertUnprocessable();
        $this->travel(61)->minutes();
        $this->postJson('/api/auth/reset-password', $input)->assertUnprocessable();
        $this->assertTrue(Hash::check('password', $user->fresh()->password));
        $this->assertTrue(Hash::check('password', $other->fresh()->password));
    }

    public function test_account_deletion_requires_fresh_token_current_password_and_confirmation(): void
    {
        $user = User::factory()->create();
        $input = ['password' => 'password', 'confirmation' => true];
        $this->deleteJson('/api/auth/account', $input)->assertUnauthorized();
        $headers = $this->headers($user);
        $this->deleteJson('/api/auth/account', ['password' => 'wrong', 'confirmation' => true], $headers)->assertUnprocessable();
        $this->deleteJson('/api/auth/account', ['password' => 'password'], $headers)->assertUnprocessable();
        $user->forceFill(['api_token_expires_at' => now()->subMinute()])->save();
        app('auth')->forgetGuards();
        $this->deleteJson('/api/auth/account', $input, $headers)->assertUnauthorized();
        $this->assertNotNull($user->fresh());
    }

    public function test_deletion_removes_identity_votes_and_sessions_but_preserves_conversations_and_other_votes(): void
    {
        $user = User::factory()->create();
        $other = User::factory()->create();
        $community = Community::query()->create(['name' => 'General', 'slug' => 'general', 'owner_user_id' => $user->id, 'member_count' => 2]);
        foreach ([$user, $other] as $member) {
            $community->memberships()->create(['user_id' => $member->id, 'role' => 'member']);
        }
        $post = Post::query()->create(['community_id' => $community->id, 'author_user_id' => $user->id, 'title' => 'Retain this', 'slug' => 'retain', 'body' => 'Original body', 'comment_count' => 1]);
        $comment = Comment::query()->create(['post_id' => $post->id, 'author_user_id' => $user->id, 'body' => 'Retain this reply', 'depth' => 0]);
        foreach ([$post, $comment] as $target) {
            $target->votes()->create(['user_id' => $user->id, 'value' => 1]);
            $target->votes()->create(['user_id' => $other->id, 'value' => -1]);
            $target->update(['score' => 0, 'upvote_count' => 1, 'downvote_count' => 1]);
        }
        DB::table('sessions')->insert(['id' => 'to-remove', 'user_id' => $user->id, 'payload' => '', 'last_activity' => time()]);
        Password::createToken($user);
        $headers = $this->headers($user);
        $this->deleteJson('/api/auth/account', ['password' => 'password', 'confirmation' => true], $headers)->assertOk()->assertJsonPath('deleted', true);
        $this->assertNull($user->fresh());
        $this->assertDatabaseMissing('votes', ['user_id' => $user->id]);
        $this->assertDatabaseCount('votes', 2);
        $this->assertDatabaseMissing('sessions', ['user_id' => $user->id]);
        $this->assertDatabaseMissing('password_reset_tokens', ['email' => $user->email]);
        $this->assertSame(1, $community->fresh()->member_count);
        $this->assertNull($community->fresh()->owner_user_id);
        foreach ([$post, $comment] as $target) {
            $target->refresh();
            $this->assertNull($target->author_user_id);
            $this->assertSame(-1, $target->score);
            $this->assertSame(0, $target->upvote_count);
            $this->assertSame(1, $target->downvote_count);
        }
        $this->assertSame('Original body', $post->fresh()->body);
        $this->assertSame('Retain this reply', $comment->fresh()->body);
        app('auth')->forgetGuards();
        $this->getJson('/api/auth/me', $headers)->assertUnauthorized();
        $this->postJson('/api/auth/login', ['email' => $user->email, 'password' => 'password'])->assertUnprocessable();
    }

    public function test_recovery_routes_are_rate_limited(): void
    {
        Notification::fake();
        for ($i = 0; $i < 10; $i++) {
            $this->postJson('/api/auth/forgot-password', ['email' => 'missing@example.test'])->assertStatus(202);
        }
        $this->postJson('/api/auth/forgot-password', ['email' => 'missing@example.test'])->assertTooManyRequests()->assertHeader('Retry-After');
    }
}
