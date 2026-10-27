<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class OnboardingTest extends TestCase
{
    use RefreshDatabase;

    private function register(): array
    {
        $response = $this->postJson('/api/auth/register', [
            'name' => 'New reader', 'username' => 'new_reader', 'email' => 'new@example.test',
            'password' => 'test-password', 'password_confirmation' => 'test-password',
        ])->assertCreated()->assertJsonPath('data.user.onboarding', ['welcome' => true, 'discuss' => true, 'vote' => true])
            ->assertJsonMissingPath('data.user.onboarding_started_at');

        return ['Authorization' => 'Bearer '.$response->json('data.token')];
    }

    public function test_new_accounts_are_enrolled_but_existing_accounts_stay_out(): void
    {
        $old = User::factory()->create(['password' => 'test-password']);
        $headers = $this->register();
        $this->getJson('/api/auth/me', $headers)->assertOk()->assertJsonPath('data.onboarding.welcome', true);
        $this->assertNull($old->fresh()->onboarding_started_at);
        $login = $this->postJson('/api/auth/login', ['email' => $old->email, 'password' => 'test-password'])->assertOk()
            ->assertJsonPath('data.user.onboarding', ['welcome' => false, 'discuss' => false, 'vote' => false]);
        $this->app['auth']->forgetGuards();
        $this->postJson('/api/auth/onboarding', ['step' => 'welcome'], ['Authorization' => 'Bearer '.$login->json('data.token')])->assertOk();
        $this->assertNull($old->fresh()->onboarding_welcome_at);
    }

    public function test_progress_is_idempotent_account_scoped_and_survives_logout_and_login(): void
    {
        $other = User::factory()->create(['onboarding_started_at' => now()]);
        $headers = $this->register();
        $this->postJson('/api/auth/onboarding', ['step' => 'discuss', 'user_id' => $other->id], $headers)
            ->assertOk()->assertJsonPath('data.discuss', false)->assertJsonPath('data.welcome', true);
        $this->assertNull($other->fresh()->onboarding_discuss_at);
        $first = User::where('email', 'new@example.test')->firstOrFail()->onboarding_discuss_at;
        $this->travel(1)->minutes();
        $this->postJson('/api/auth/onboarding', ['step' => 'discuss'], $headers)->assertOk();
        $this->postJson('/api/auth/onboarding', ['step' => 'welcome'], $headers)->assertOk();
        $this->assertTrue($first->equalTo(User::where('email', 'new@example.test')->firstOrFail()->onboarding_discuss_at));
        $this->postJson('/api/auth/logout', [], $headers)->assertOk();
        $login = $this->postJson('/api/auth/login', ['email' => 'new@example.test', 'password' => 'test-password'])->assertOk()
            ->assertJsonPath('data.user.onboarding', ['welcome' => false, 'discuss' => false, 'vote' => true]);
        $this->app['auth']->forgetGuards();
        $this->getJson('/api/auth/me', ['Authorization' => 'Bearer '.$login->json('data.token')])->assertOk()
            ->assertJsonPath('data.onboarding.discuss', false)->assertJsonMissingPath('data.onboarding_discuss_at');
    }

    public function test_hints_expire_at_seven_days_without_reenrolling_or_completing_steps(): void
    {
        $this->travelTo(now()->startOfSecond());
        $this->register();
        $user = User::where('email', 'new@example.test')->firstOrFail();
        $started = $user->onboarding_started_at;
        $this->travelTo($started->addDays(7)->subSecond());
        $this->assertTrue($user->onboardingState()['discuss']);
        $this->travelTo($started->addDays(7));
        $this->assertSame(['welcome' => true, 'discuss' => false, 'vote' => false], $user->onboardingState());
        $this->assertNull($user->fresh()->onboarding_vote_at);
        $this->assertTrue($started->equalTo($user->onboarding_started_at));
    }

    public function test_only_supported_authenticated_events_are_accepted(): void
    {
        $this->postJson('/api/auth/onboarding', ['step' => 'welcome'])->assertUnauthorized();
        $headers = $this->register();
        $this->app['auth']->forgetGuards();
        foreach (['vote', 'reset', 'onboarding_started_at', ['welcome']] as $step) {
            $this->postJson('/api/auth/onboarding', ['step' => $step], $headers)->assertUnprocessable();
        }
        $user = User::where('email', 'new@example.test')->firstOrFail();
        $user->forceFill(['api_token_expires_at' => now()->subSecond()])->save();
        $this->app['auth']->forgetGuards();
        $this->postJson('/api/auth/onboarding', ['step' => 'welcome'], $headers)->assertUnauthorized();
        $this->assertNull($user->fresh()->onboarding_welcome_at);
    }

    public function test_only_successful_nonzero_votes_complete_the_hint_and_removal_never_reopens_it(): void
    {
        $headers = $this->register();
        $user = User::where('email', 'new@example.test')->firstOrFail();
        $community = Community::create(['owner_user_id' => $user->id, 'name' => 'General', 'slug' => 'general', 'is_private' => false]);
        $post = Post::create(['community_id' => $community->id, 'author_user_id' => $user->id, 'title' => 'A source discussion', 'slug' => 'source-discussion', 'body' => 'Context', 'is_locked' => true]);
        $comment = Comment::create(['post_id' => $post->id, 'author_user_id' => $user->id, 'body' => 'A reply', 'depth' => 0]);
        $input = ['votable_type' => 'comment', 'votable_id' => $comment->id, 'post_id' => $post->id];
        $this->postJson('/api/votes', $input + ['value' => 1], $headers)->assertStatus(423);
        $this->assertNull($user->fresh()->onboarding_vote_at);
        $post->update(['is_locked' => false]);
        $this->postJson('/api/votes', $input + ['value' => 0], $headers)->assertOk();
        $this->assertNull($user->fresh()->onboarding_vote_at);
        $this->postJson('/api/votes', $input + ['value' => -1], $headers)->assertOk();
        $firstVote = $user->fresh()->onboarding_vote_at;
        $this->assertNotNull($firstVote);
        $this->travel(1)->minutes();
        $this->postJson('/api/votes', $input + ['value' => -1], $headers)->assertOk()->assertJsonPath('data.changed', false);
        $this->postJson('/api/votes', $input + ['value' => 0], $headers)->assertOk();
        $this->app['auth']->forgetGuards();
        $this->getJson('/api/auth/me', $headers)->assertOk()->assertJsonPath('data.onboarding.vote', false);
        $this->assertTrue($firstVote->equalTo($user->fresh()->onboarding_vote_at));
    }
}
