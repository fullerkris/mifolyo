<?php

namespace Tests\Feature;

use App\Models\Community;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Str;
use Tests\TestCase;

class CommunityEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_community_can_be_created_by_authenticated_user(): void
    {
        $user = User::factory()->create();
        $headers = $this->headersForUser($user);

        $response = $this->postJson('/api/communities', [
            'name' => 'MiFolyo Builders',
            'description' => 'Build notes and updates.',
            'is_private' => false,
        ], $headers);

        $response
            ->assertCreated()
            ->assertJsonPath('data.name', 'MiFolyo Builders')
            ->assertJsonPath('data.slug', 'mifolyo-builders');

        $this->assertDatabaseHas('communities', [
            'name' => 'MiFolyo Builders',
            'owner_user_id' => $user->id,
            'member_count' => 1,
        ]);

        $this->assertDatabaseHas('community_memberships', [
            'community_id' => $response->json('data.id'),
            'user_id' => $user->id,
            'role' => 'owner',
        ]);
    }

    public function test_public_communities_are_listed(): void
    {
        Community::query()->create([
            'name' => 'Public Forum',
            'slug' => 'public-forum',
            'is_private' => false,
        ]);

        Community::query()->create([
            'name' => 'Private Forum',
            'slug' => 'private-forum',
            'is_private' => true,
        ]);

        $response = $this->getJson('/api/communities');

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame('public-forum', $response->json('data.0.slug'));
    }

    public function test_join_is_idempotent_for_membership_creation(): void
    {
        $owner = User::factory()->create();
        $member = User::factory()->create();

        $community = Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'General',
            'slug' => 'general',
            'is_private' => false,
            'member_count' => 0,
        ]);

        $headers = $this->headersForUser($member);

        $this->postJson('/api/communities/general/join', [], $headers)
            ->assertOk()
            ->assertJsonPath('data.joined', true);

        $this->postJson('/api/communities/general/join', [], $headers)
            ->assertOk()
            ->assertJsonPath('data.joined', false);

        $this->assertDatabaseCount('community_memberships', 1);
        $this->assertDatabaseHas('communities', [
            'id' => $community->id,
            'member_count' => 1,
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
