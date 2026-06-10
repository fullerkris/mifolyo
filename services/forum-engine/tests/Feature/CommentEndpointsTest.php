<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class CommentEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_authenticated_user_can_create_comment_and_reply(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);
        $headers = $this->headersForUser($user);

        $rootResponse = $this->postJson('/api/posts/'.$post->id.'/comments', [
            'body' => 'Root comment',
        ], $headers);

        $rootId = $rootResponse->json('data.id');

        $rootResponse
            ->assertCreated()
            ->assertJsonPath('data.body', 'Root comment')
            ->assertJsonPath('data.depth', 0);

        $this->postJson('/api/posts/'.$post->id.'/comments', [
            'body' => 'Child comment',
            'parent_comment_id' => $rootId,
        ], $headers)
            ->assertCreated()
            ->assertJsonPath('data.depth', 1);
    }

    public function test_threaded_comments_are_returned_nested_by_parent(): void
    {
        $user = User::factory()->create();
        $post = $this->makePost($user);

        $root = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $user->id,
            'body' => 'Root',
            'depth' => 0,
        ]);

        Comment::query()->create([
            'post_id' => $post->id,
            'parent_comment_id' => $root->id,
            'author_user_id' => $user->id,
            'body' => 'Child',
            'depth' => 1,
        ]);

        $response = $this->getJson('/api/posts/'.$post->id.'/comments');

        $response
            ->assertOk()
            ->assertJsonPath('data.0.body', 'Root')
            ->assertJsonPath('data.0.children.0.body', 'Child');
    }

    public function test_non_member_cannot_comment_in_private_community(): void
    {
        $owner = User::factory()->create();
        $otherUser = User::factory()->create();
        $post = $this->makePost($owner, isPrivate: true);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $owner->id,
            'role' => 'owner',
        ]);

        $this->postJson('/api/posts/'.$post->id.'/comments', [
            'body' => 'Should fail',
        ], $this->headersForUser($otherUser))
            ->assertStatus(403)
            ->assertJsonPath('message', 'You must be a member to comment in this private community.');
    }

    private function makePost(User $user, bool $isPrivate = false): Post
    {
        $community = Community::query()->create([
            'owner_user_id' => $user->id,
            'name' => 'General '.Str::random(6),
            'slug' => 'general-'.Str::lower(Str::random(6)),
            'is_private' => $isPrivate,
        ]);

        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $user->id,
            'title' => 'First post',
            'slug' => 'first-post',
            'body' => 'Body text',
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
