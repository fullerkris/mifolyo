<?php

namespace Tests\Feature;

use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class PostEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_authenticated_user_can_create_post(): void
    {
        $user = User::factory()->create();
        $community = Community::query()->create([
            'owner_user_id' => $user->id,
            'name' => 'Product',
            'slug' => 'product',
            'is_private' => false,
        ]);

        $response = $this->postJson('/api/posts', [
            'community_slug' => $community->slug,
            'title' => 'First update',
            'content_type' => 'text',
            'body' => 'Shipping v1 this week.',
        ], $this->headersForUser($user));

        $response
            ->assertCreated()
            ->assertJsonPath('data.title', 'First update')
            ->assertJsonPath('data.community.slug', 'product')
            ->assertJsonPath('data.author.id', $user->id);

        $this->assertDatabaseHas('posts', [
            'community_id' => $community->id,
            'author_user_id' => $user->id,
            'slug' => 'first-update',
        ]);
    }

    public function test_post_can_be_retrieved_by_id(): void
    {
        $user = User::factory()->create();
        $community = Community::query()->create([
            'owner_user_id' => $user->id,
            'name' => 'General',
            'slug' => 'general',
            'is_private' => false,
        ]);

        $post = Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $user->id,
            'title' => 'Welcome',
            'slug' => 'welcome',
            'body' => 'Hello MiFolyo',
            'content_type' => 'text',
            'published_at' => now(),
        ]);

        $this->getJson('/api/posts/'.$post->id)
            ->assertOk()
            ->assertJsonPath('data.slug', 'welcome')
            ->assertJsonPath('data.community.slug', 'general');
    }

    public function test_non_member_cannot_post_to_private_community(): void
    {
        $owner = User::factory()->create();
        $otherUser = User::factory()->create();

        $community = Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'Private Club',
            'slug' => 'private-club',
            'is_private' => true,
        ]);

        CommunityMembership::query()->create([
            'community_id' => $community->id,
            'user_id' => $owner->id,
            'role' => 'owner',
        ]);

        $this->postJson('/api/posts', [
            'community_slug' => $community->slug,
            'title' => 'Secret update',
            'content_type' => 'text',
            'body' => 'Should not be allowed.',
        ], $this->headersForUser($otherUser))
            ->assertStatus(403)
            ->assertJsonPath('message', 'You must be a member to post in this private community.');
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
