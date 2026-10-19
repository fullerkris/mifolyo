<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use App\Models\Vote;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;
use Tests\TestCase;

class VoteEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_vote_on_post_updates_score_and_counters(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $headers = $this->headersForUser($user);

        $this->postJson('/api/votes', [
            'votable_type' => 'post',
            'votable_id' => $post->id,
            'value' => 1,
        ], $headers)
            ->assertOk()
            ->assertJsonPath('data.changed', true)
            ->assertJsonPath('data.votable.score', 1)
            ->assertJsonPath('data.votable.upvote_count', 1)
            ->assertJsonPath('data.votable.downvote_count', 0);

        $this->postJson('/api/votes', [
            'votable_type' => 'post',
            'votable_id' => $post->id,
            'value' => -1,
        ], $headers)
            ->assertOk()
            ->assertJsonPath('data.changed', true)
            ->assertJsonPath('data.votable.score', -1)
            ->assertJsonPath('data.votable.upvote_count', 0)
            ->assertJsonPath('data.votable.downvote_count', 1);

        $this->assertDatabaseCount('votes', 1);
    }

    public function test_duplicate_vote_value_is_idempotent(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $headers = $this->headersForUser($user);

        $this->postJson('/api/votes', [
            'votable_type' => 'post',
            'votable_id' => $post->id,
            'value' => 1,
        ], $headers)->assertOk();

        $this->postJson('/api/votes', [
            'votable_type' => 'post',
            'votable_id' => $post->id,
            'value' => 1,
        ], $headers)
            ->assertOk()
            ->assertJsonPath('data.changed', false)
            ->assertJsonPath('data.votable.score', 1);

        $this->assertDatabaseCount('votes', 1);
    }

    public function test_vote_on_comment_updates_comment_counts(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $comment = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $user->id,
            'body' => 'Hello',
            'depth' => 0,
        ]);

        $this->postJson('/api/votes', [
            'votable_type' => 'comment',
            'votable_id' => $comment->id,
            'value' => 1,
        ], $this->headersForUser($user))
            ->assertOk()
            ->assertJsonPath('data.votable.score', 1)
            ->assertJsonPath('data.votable.upvote_count', 1);

        $vote = Vote::query()->first();
        $this->assertSame(Comment::class, $vote->votable_type);
        $this->assertSame($comment->id, $vote->votable_id);
    }

    public function test_setting_changing_and_removing_votes_is_idempotent_for_both_types(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $comment = Comment::query()->create(['post_id' => $post->id, 'author_user_id' => $user->id, 'body' => 'Reply', 'depth' => 0]);
        $headers = $this->headersForUser($user);
        foreach (['post' => $post, 'comment' => $comment] as $type => $target) {
            foreach ([[0, false], [1, true], [1, false], [-1, true], [-1, false], [0, true], [0, false]] as [$value, $changed]) {
                $this->postJson('/api/votes', ['votable_type' => $type, 'votable_id' => $target->id, 'value' => $value, 'post_id' => $post->id], $headers)
                    ->assertOk()->assertJsonPath('data.changed', $changed)->assertJsonPath('data.viewer_vote', $value)
                    ->assertJsonPath('data.votable.score', $value)
                    ->assertJsonPath('data.votable.upvote_count', $value === 1 ? 1 : 0)
                    ->assertJsonPath('data.votable.downvote_count', $value === -1 ? 1 : 0);
                $this->assertSame($value, $target->fresh()->score);
                $this->assertDatabaseCount('votes', $value === 0 ? 0 : 1);
            }
        }
    }

    public function test_read_state_is_viewer_specific_private_and_batched_for_nested_comments(): void
    {
        $first = User::factory()->create();
        $second = User::factory()->create();
        $post = $this->makePost($first);
        $parent = Comment::query()->create(['post_id' => $post->id, 'author_user_id' => $first->id, 'body' => 'Parent', 'depth' => 0]);
        $child = Comment::query()->create(['post_id' => $post->id, 'author_user_id' => $first->id, 'body' => 'Nested', 'parent_comment_id' => $parent->id, 'depth' => 1]);
        foreach ([$first->id => 1, $second->id => -1] as $userId => $value) {
            foreach ([$post, $parent, $child] as $target) {
                $target->votes()->create(['user_id' => $userId, 'value' => $value]);
            }
        }
        foreach ([[$first, 1], [$second, -1], [null, 0]] as [$viewer, $value]) {
            app('auth')->forgetGuards();
            $headers = $viewer ? $this->headersForUser($viewer) : [];
            $response = $this->getJson('/api/posts/'.$post->id, $headers)->assertOk()->assertJsonPath('data.viewer_vote', $value);
            $this->assertStringContainsString('no-store', $response->headers->get('Cache-Control'));
            DB::enableQueryLog();
            DB::flushQueryLog();
            $response = $this->getJson('/api/posts/'.$post->id.'/comments', $headers)->assertOk()
                ->assertJsonPath('data.0.viewer_vote', $value)->assertJsonPath('data.0.children.0.viewer_vote', $value);
            $voteQueries = array_filter(DB::getQueryLog(), fn ($query) => str_contains($query['query'], '"votes"'));
            DB::disableQueryLog();
            $this->assertCount($viewer ? 1 : 0, $voteQueries);
            $this->assertStringContainsString('no-store', $response->headers->get('Cache-Control'));
            $response->assertJsonMissingPath('data.0.votes');
        }
    }

    public function test_locked_removed_or_wrong_thread_targets_cannot_change_scores(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $comment = Comment::query()->create(['post_id' => $post->id, 'author_user_id' => $user->id, 'body' => 'Reply', 'depth' => 0]);
        $headers = $this->headersForUser($user);
        foreach (['is_locked', 'is_removed'] as $flag) {
            foreach ([$post, $comment] as $closed) {
                $closed->update([$flag => true]);
                foreach ([-1, 0, 1] as $value) {
                    $this->postJson('/api/votes', ['votable_type' => 'comment', 'votable_id' => $comment->id, 'value' => $value], $headers)->assertStatus(423);
                    if ($closed instanceof Post) {
                        $this->postJson('/api/votes', ['votable_type' => 'post', 'votable_id' => $post->id, 'value' => $value], $headers)->assertStatus(423);
                    }
                }
                $closed->update([$flag => false]);
            }
        }
        $this->postJson('/api/votes', ['votable_type' => 'comment', 'votable_id' => $comment->id, 'post_id' => $post->id + 1, 'value' => 1], $headers)->assertUnprocessable();
        $this->assertDatabaseCount('votes', 0);
        $this->assertSame(0, $post->fresh()->score);
        $this->assertSame(0, $comment->fresh()->score);
    }

    public function test_voting_requires_fresh_authentication_and_private_membership(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $post->community->update(['is_private' => true]);
        $input = ['votable_type' => 'post', 'votable_id' => $post->id, 'value' => 1];
        $this->postJson('/api/votes', $input)->assertUnauthorized();
        $headers = $this->headersForUser($user);
        app('auth')->forgetGuards();
        $this->postJson('/api/votes', $input, $headers)->assertForbidden();
        $post->community->memberships()->create(['user_id' => $user->id, 'role' => 'member', 'joined_at' => now()]);
        $this->postJson('/api/votes', $input, $headers)->assertOk();
        $user->forceFill(['api_token_expires_at' => now()->subMinute()])->save();
        app('auth')->forgetGuards();
        $this->postJson('/api/votes', array_replace($input, ['value' => 0]), $headers)->assertUnauthorized();
        $this->assertSame(1, $post->fresh()->score);
    }

    private function makePost(User $user): Post
    {
        $community = Community::query()->create([
            'owner_user_id' => $user->id,
            'name' => 'General '.Str::random(6),
            'slug' => 'general-'.Str::lower(Str::random(6)),
            'is_private' => false,
        ]);

        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $user->id,
            'title' => 'Voting post',
            'slug' => 'voting-post',
            'body' => 'Vote body',
            'content_type' => 'text',
            'published_at' => now(),
        ]);
    }

    private function headersForUser(User $user): array
    {
        $token = Str::random(60);

        $user->forceFill([
            'api_token' => hash('sha256', $token),
            'api_token_expires_at' => now()->addHour(),
        ])->save();

        return [
            'Authorization' => 'Bearer '.$token,
        ];
    }
}
