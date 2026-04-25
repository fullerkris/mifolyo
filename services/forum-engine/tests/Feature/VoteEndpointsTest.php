<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use App\Models\Vote;
use Illuminate\Foundation\Testing\RefreshDatabase;
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
            'api_token' => $token,
        ])->save();

        return [
            'Authorization' => 'Bearer '.$token,
        ];
    }
}
