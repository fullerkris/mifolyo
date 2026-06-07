<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Gate;
use Illuminate\Support\Str;
use Tests\TestCase;

class PolicyAuthorizationTest extends TestCase
{
    use RefreshDatabase;

    public function test_post_author_can_update_own_post(): void
    {
        $author = User::factory()->create();
        $post = $this->makePost($author);

        $this->assertTrue(Gate::forUser($author)->allows('update', $post));
    }

    public function test_community_moderator_can_lock_post(): void
    {
        $owner = User::factory()->create();
        $moderator = User::factory()->create();
        $post = $this->makePost($owner);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        $this->assertTrue(Gate::forUser($moderator)->allows('lock', $post));
    }

    public function test_regular_user_cannot_lock_post(): void
    {
        $owner = User::factory()->create();
        $otherUser = User::factory()->create();
        $post = $this->makePost($owner);

        $this->assertFalse(Gate::forUser($otherUser)->allows('lock', $post));
    }

    public function test_comment_author_can_delete_own_comment(): void
    {
        $author = User::factory()->create();
        $post = $this->makePost($author);

        $comment = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $author->id,
            'body' => 'Author comment',
            'depth' => 0,
        ]);

        $this->assertTrue(Gate::forUser($author)->allows('delete', $comment));
    }

    public function test_community_owner_can_remove_comment(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();
        $post = $this->makePost($owner);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $owner->id,
            'role' => 'owner',
        ]);

        $comment = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $member->id,
            'body' => 'Member comment',
            'depth' => 0,
        ]);

        $this->assertTrue(Gate::forUser($owner)->allows('remove', $comment));
    }

    private function makePost(User $owner): Post
    {
        $community = Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'General '.Str::random(6),
            'slug' => 'general-'.Str::lower(Str::random(6)),
        ]);

        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $owner->id,
            'title' => 'Policy post',
            'slug' => 'policy-post',
            'body' => 'Policy body',
            'content_type' => 'text',
            'published_at' => now(),
        ]);
    }
}
