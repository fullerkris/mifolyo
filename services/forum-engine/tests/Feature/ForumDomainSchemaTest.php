<?php

namespace Tests\Feature;

use App\Models\Comment;
use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use App\Models\Vote;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class ForumDomainSchemaTest extends TestCase
{
    use RefreshDatabase;

    public function test_forum_domain_models_store_and_load_relationships(): void
    {
        $user = User::factory()->create();

        $community = Community::create([
            'owner_user_id' => $user->id,
            'name' => 'General',
            'slug' => 'general',
            'description' => 'General discussion',
        ]);

        $membership = CommunityMembership::create([
            'community_id' => $community->id,
            'user_id' => $user->id,
            'role' => 'owner',
        ]);

        $post = Post::create([
            'community_id' => $community->id,
            'author_user_id' => $user->id,
            'title' => 'Welcome to MiFolyo',
            'slug' => 'welcome-to-mifolyo',
            'body' => 'First thread in this community.',
            'content_type' => 'text',
        ]);

        $rootComment = Comment::create([
            'post_id' => $post->id,
            'author_user_id' => $user->id,
            'body' => 'Root comment',
            'depth' => 0,
        ]);

        $childComment = Comment::create([
            'post_id' => $post->id,
            'parent_comment_id' => $rootComment->id,
            'author_user_id' => $user->id,
            'body' => 'Child comment',
            'depth' => 1,
        ]);

        $vote = Vote::create([
            'user_id' => $user->id,
            'votable_type' => Post::class,
            'votable_id' => $post->id,
            'value' => 1,
        ]);

        $this->assertTrue($community->owner->is($user));
        $this->assertTrue($membership->community->is($community));
        $this->assertTrue($membership->user->is($user));
        $this->assertTrue($post->community->is($community));
        $this->assertTrue($post->author->is($user));
        $this->assertTrue($rootComment->post->is($post));
        $this->assertTrue($childComment->parent->is($rootComment));
        $this->assertTrue($vote->votable->is($post));
    }
}
