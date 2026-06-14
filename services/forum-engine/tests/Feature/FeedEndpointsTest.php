<?php

namespace Tests\Feature;

use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class FeedEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_home_feed_returns_public_posts_only_for_anonymous_user(): void
    {
        $owner = User::factory()->create();
        $publicCommunity = $this->makeCommunity($owner, isPrivate: false);
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);

        $publicPost = $this->makePost($publicCommunity, $owner, 'public-post');
        $this->makePost($privateCommunity, $owner, 'private-post');

        $response = $this->getJson('/api/feeds/home?sort=new');

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame($publicPost->id, $response->json('data.0.id'));
    }

    public function test_home_feed_includes_private_memberships_for_authenticated_user(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();
        $publicCommunity = $this->makeCommunity($owner, isPrivate: false);
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);

        $publicPost = $this->makePost($publicCommunity, $owner, 'public-post');
        $privatePost = $this->makePost($privateCommunity, $owner, 'private-post');

        CommunityMembership::query()->create([
            'community_id' => $privateCommunity->id,
            'user_id' => $member->id,
            'role' => 'member',
        ]);

        $response = $this->getJson('/api/feeds/home?sort=new', $this->headersForUser($member));

        $response->assertOk();
        $ids = collect($response->json('data'))->pluck('id')->all();

        $this->assertContains($publicPost->id, $ids);
        $this->assertContains($privatePost->id, $ids);
    }

    public function test_community_feed_requires_membership_for_private_community(): void
    {
        $owner = User::factory()->create();
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);
        $this->makePost($privateCommunity, $owner, 'private-post');

        $this->getJson('/api/feeds/community/'.$privateCommunity->slug)
            ->assertStatus(403)
            ->assertJsonPath('message', 'This community is private.');
    }

    public function test_community_feed_allows_member_access_to_private_community(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);
        $post = $this->makePost($privateCommunity, $owner, 'private-post');

        CommunityMembership::query()->create([
            'community_id' => $privateCommunity->id,
            'user_id' => $member->id,
            'role' => 'member',
        ]);

        $response = $this->getJson('/api/feeds/community/'.$privateCommunity->slug, $this->headersForUser($member));

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame($post->id, $response->json('data.0.id'));
    }

    public function test_feed_sort_top_orders_by_score(): void
    {
        $owner = User::factory()->create();
        $community = $this->makeCommunity($owner, isPrivate: false);

        $low = $this->makePost($community, $owner, 'low-score');
        $high = $this->makePost($community, $owner, 'high-score');

        $low->update(['score' => 2]);
        $high->update(['score' => 10]);

        $response = $this->getJson('/api/feeds/community/'.$community->slug.'?sort=top');

        $response->assertOk();
        $this->assertSame($high->id, $response->json('data.0.id'));
        $this->assertSame($low->id, $response->json('data.1.id'));
    }

    private function makeCommunity(User $owner, bool $isPrivate): Community
    {
        return Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'Community '.Str::random(6),
            'slug' => 'community-'.Str::lower(Str::random(6)),
            'is_private' => $isPrivate,
        ]);
    }

    private function makePost(Community $community, User $author, string $slug): Post
    {
        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $author->id,
            'title' => Str::headline(str_replace('-', ' ', $slug)),
            'slug' => $slug,
            'body' => 'Post body',
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
