<?php

namespace Tests\Unit;

use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use App\Services\FeedService;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class FeedServiceTest extends TestCase
{
    use RefreshDatabase;

    private FeedService $service;

    protected function setUp(): void
    {
        parent::setUp();

        $this->service = new FeedService;
    }

    public function test_community_feed_top_sort_orders_by_score_desc(): void
    {
        $owner = User::factory()->create();
        $community = $this->makeCommunity($owner, false);

        $low = $this->makePost($community, $owner, 'low-score', now()->subHour(), score: 2, commentCount: 1);
        $high = $this->makePost($community, $owner, 'high-score', now()->subMinutes(30), score: 12, commentCount: 0);

        $results = $this->service->communityFeed($community, 'top', 20)->items();

        $this->assertSame($high->id, $results[0]->id);
        $this->assertSame($low->id, $results[1]->id);
    }

    public function test_community_feed_new_sort_orders_by_published_at_desc(): void
    {
        $owner = User::factory()->create();
        $community = $this->makeCommunity($owner, false);

        $old = $this->makePost($community, $owner, 'old-post', now()->subDay(), score: 100, commentCount: 100);
        $new = $this->makePost($community, $owner, 'new-post', now(), score: 0, commentCount: 0);

        $results = $this->service->communityFeed($community, 'new', 20)->items();

        $this->assertSame($new->id, $results[0]->id);
        $this->assertSame($old->id, $results[1]->id);
    }

    public function test_home_feed_hot_sort_uses_score_and_comment_weighting(): void
    {
        $owner = User::factory()->create();
        $community = $this->makeCommunity($owner, false);

        $first = $this->makePost($community, $owner, 'first', now()->subHour(), score: 3, commentCount: 1);
        $second = $this->makePost($community, $owner, 'second', now()->subHour(), score: 1, commentCount: 8);
        $third = $this->makePost($community, $owner, 'third', now()->subHour(), score: 4, commentCount: 0);

        $results = $this->service->homeFeed(null, 'hot', 20)->items();

        $this->assertSame($second->id, $results[0]->id);
        $this->assertSame($third->id, $results[1]->id);
        $this->assertSame($first->id, $results[2]->id);
    }

    public function test_can_view_community_feed_requires_membership_for_private_community(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();
        $outsider = User::factory()->create();
        $community = $this->makeCommunity($owner, true);

        CommunityMembership::query()->create([
            'community_id' => $community->id,
            'user_id' => $member->id,
            'role' => 'member',
        ]);

        $this->assertFalse($this->service->canViewCommunityFeed($community, $outsider));
        $this->assertTrue($this->service->canViewCommunityFeed($community, $member));
    }

    private function makeCommunity(User $owner, bool $isPrivate): Community
    {
        return Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'Community '.Str::random(6),
            'slug' => 'community-'.Str::lower(Str::random(6)),
            'is_private' => $isPrivate,
            'is_nsfw' => false,
        ]);
    }

    private function makePost(
        Community $community,
        User $author,
        string $slug,
        \DateTimeInterface $publishedAt,
        int $score,
        int $commentCount
    ): Post {
        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $author->id,
            'title' => Str::headline($slug),
            'slug' => $slug,
            'body' => 'Body',
            'content_type' => 'text',
            'published_at' => $publishedAt,
            'score' => $score,
            'comment_count' => $commentCount,
        ]);
    }
}
