<?php

namespace Tests\Feature;

use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use App\Support\SourceUrlNormalizer;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class ThreadByUrlEndpointTest extends TestCase
{
    use RefreshDatabase;

    public function test_authenticated_user_can_create_url_attached_thread(): void
    {
        $author = User::factory()->create(['level' => 2]);
        $community = $this->makeCommunity($author, isPrivate: false);
        $sourceUrl = 'https://Example.com:443/articles/source-quality?ref=search#methodology';
        $source = SourceUrlNormalizer::normalize($sourceUrl);

        $response = $this->postJson('/api/threads', [
            'community_slug' => $community->slug,
            'title' => 'Can we trust this methodology?',
            'body' => 'The citations look strong, but the sample size is unclear.',
            'source_url' => $sourceUrl,
            'source_url_hash' => 'client-supplied-value-must-not-win',
            'source_domain' => 'evil.example',
        ], $this->headersForUser($author));

        $response
            ->assertCreated()
            ->assertJsonPath('data.title', 'Can we trust this methodology?')
            ->assertJsonPath('data.slug', 'can-we-trust-this-methodology')
            ->assertJsonPath('data.source_url', 'https://example.com/articles/source-quality?ref=search')
            ->assertJsonPath('data.source_domain', 'example.com')
            ->assertJsonPath('data.source_path', '/articles/source-quality?ref=search')
            ->assertJsonPath('data.author.username', $author->username)
            ->assertJsonPath('data.author.level', 2)
            ->assertJsonPath('data.community.slug', $community->slug);

        $this->assertDatabaseHas('posts', array_merge([
            'community_id' => $community->id,
            'author_user_id' => $author->id,
            'title' => 'Can we trust this methodology?',
            'slug' => 'can-we-trust-this-methodology',
            'url' => $source['source_url'],
            'content_type' => 'link',
        ], $source));

        $this->assertDatabaseHas('communities', [
            'id' => $community->id,
            'post_count' => 1,
        ]);
    }

    public function test_multiple_threads_can_be_created_for_the_same_source_url(): void
    {
        $author = User::factory()->create();
        $community = $this->makeCommunity($author, isPrivate: false);
        $sourceUrl = 'https://example.com/articles/source-quality';
        $headers = $this->headersForUser($author);

        $first = $this->postJson('/api/threads', [
            'community_slug' => $community->slug,
            'title' => 'Different angle',
            'body' => 'First take.',
            'source_url' => $sourceUrl,
        ], $headers);

        $second = $this->postJson('/api/threads', [
            'community_slug' => $community->slug,
            'title' => 'Different angle',
            'body' => 'Second take.',
            'source_url' => $sourceUrl.'#section-two',
        ], $headers);

        $first->assertCreated()->assertJsonPath('data.slug', 'different-angle');
        $second->assertCreated()->assertJsonPath('data.slug', 'different-angle-2');

        $this->getJson('/api/threads/by-url?url='.urlencode($sourceUrl))
            ->assertOk()
            ->assertJsonPath('meta.total', 2);
    }

    public function test_non_member_cannot_create_thread_in_private_community(): void
    {
        $owner = User::factory()->create();
        $otherUser = User::factory()->create();
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);

        $this->postJson('/api/threads', [
            'community_slug' => $privateCommunity->slug,
            'title' => 'Private source review',
            'source_url' => 'https://example.com/private-source',
        ], $this->headersForUser($otherUser))
            ->assertStatus(403)
            ->assertJsonPath('message', 'You must be a member to create a thread in this private community.');
    }

    public function test_threads_by_url_returns_threads_for_equivalent_normalized_url(): void
    {
        $author = User::factory()->create(['level' => 3]);
        $community = $this->makeCommunity($author, isPrivate: false);
        $source = SourceUrlNormalizer::normalize('https://Example.com:443/articles/source-quality?ref=search#section');

        $lowerScore = $this->makePost($community, $author, 'lower-score-thread', $source, [
            'score' => 2,
            'upvote_count' => 2,
            'comment_count' => 4,
        ]);
        $higherScore = $this->makePost($community, $author, 'higher-score-thread', $source, [
            'score' => 8,
            'upvote_count' => 8,
            'comment_count' => 1,
        ]);

        $this->makePost(
            $community,
            $author,
            'different-url-thread',
            SourceUrlNormalizer::normalize('https://example.com/other-page')
        );

        $response = $this->getJson('/api/threads/by-url?url='.urlencode('https://example.com/articles/source-quality?ref=search#different'));

        $response
            ->assertOk()
            ->assertJsonPath('meta.source_url', 'https://example.com/articles/source-quality?ref=search')
            ->assertJsonPath('meta.source_domain', 'example.com')
            ->assertJsonPath('meta.source_path', '/articles/source-quality?ref=search')
            ->assertJsonPath('meta.total', 2)
            ->assertJsonPath('data.0.id', $higherScore->id)
            ->assertJsonPath('data.0.author.username', $author->username)
            ->assertJsonPath('data.0.author.level', 3)
            ->assertJsonPath('data.1.id', $lowerScore->id);
    }

    public function test_threads_by_url_hides_private_community_threads_from_anonymous_users(): void
    {
        $owner = User::factory()->create();
        $publicCommunity = $this->makeCommunity($owner, isPrivate: false);
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);
        $source = SourceUrlNormalizer::normalize('https://example.com/private-test');

        $publicPost = $this->makePost($publicCommunity, $owner, 'public-thread', $source);
        $this->makePost($privateCommunity, $owner, 'private-thread', $source);

        $response = $this->getJson('/api/threads/by-url?url='.urlencode($source['source_url']));

        $response
            ->assertOk()
            ->assertJsonPath('meta.total', 1)
            ->assertJsonPath('data.0.id', $publicPost->id);
    }

    public function test_threads_by_url_includes_private_threads_for_members(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();
        $privateCommunity = $this->makeCommunity($owner, isPrivate: true);
        $source = SourceUrlNormalizer::normalize('https://example.com/member-only');
        $post = $this->makePost($privateCommunity, $owner, 'private-thread', $source);

        CommunityMembership::query()->create([
            'community_id' => $privateCommunity->id,
            'user_id' => $member->id,
            'role' => 'member',
        ]);

        $response = $this->getJson('/api/threads/by-url?url='.urlencode($source['source_url']), $this->headersForUser($member));

        $response
            ->assertOk()
            ->assertJsonPath('meta.total', 1)
            ->assertJsonPath('data.0.id', $post->id);
    }

    public function test_threads_by_url_requires_valid_http_url(): void
    {
        $this->getJson('/api/threads/by-url?url='.urlencode('ftp://example.com/file'))
            ->assertStatus(422)
            ->assertJsonValidationErrors(['url']);
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

    /**
     * @param array{source_url: string, source_url_hash: string, source_domain: string, source_path: string} $source
     */
    private function makePost(Community $community, User $author, string $slug, array $source, array $overrides = []): Post
    {
        return Post::query()->create(array_merge([
            'community_id' => $community->id,
            'author_user_id' => $author->id,
            'title' => Str::headline(str_replace('-', ' ', $slug)),
            'slug' => $slug,
            'body' => 'Post body',
            'url' => $source['source_url'],
            'content_type' => 'link',
            'published_at' => now(),
        ], $source, $overrides));
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
