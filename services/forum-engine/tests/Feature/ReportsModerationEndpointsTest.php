<?php

namespace Tests\Feature;

use App\Jobs\ProcessModerationActionJob;
use App\Jobs\ProcessReportCreatedJob;
use App\Models\Comment;
use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\ModerationAction;
use App\Models\Post;
use App\Models\Report;
use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\Queue;
use Illuminate\Support\Str;
use Tests\TestCase;

class ReportsModerationEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_authenticated_user_can_create_report_for_post(): void
    {
        $owner = User::factory()->create();
        $reporter = User::factory()->create();

        $post = $this->makePost($owner);

        $response = $this->postJson('/api/reports', [
            'reportable_type' => 'post',
            'reportable_id' => $post->id,
            'reason' => 'Spam',
            'details' => 'Looks like repeated promotional links.',
        ], $this->headersForUser($reporter));

        $response
            ->assertCreated()
            ->assertJsonPath('data.reason', 'Spam')
            ->assertJsonPath('data.status', 'open');

        $this->assertDatabaseHas('reports', [
            'reporter_user_id' => $reporter->id,
            'community_id' => $post->community_id,
            'reportable_type' => Post::class,
            'reportable_id' => $post->id,
        ]);
    }

    public function test_creating_report_dispatches_async_follow_up_job(): void
    {
        Queue::fake();

        $owner = User::factory()->create();
        $reporter = User::factory()->create();

        $post = $this->makePost($owner);

        $this->postJson('/api/reports', [
            'reportable_type' => 'post',
            'reportable_id' => $post->id,
            'reason' => 'Spam',
        ], $this->headersForUser($reporter))->assertCreated();

        Queue::assertPushed(ProcessReportCreatedJob::class, 1);
    }

    public function test_moderator_queue_returns_reports_for_moderated_communities_only(): void
    {
        $moderator = User::factory()->create();
        $ownerA = User::factory()->create();
        $ownerB = User::factory()->create();
        $reporter = User::factory()->create();

        $postA = $this->makePost($ownerA);
        $postB = $this->makePost($ownerB);

        CommunityMembership::query()->create([
            'community_id' => $postA->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        Report::query()->create([
            'reporter_user_id' => $reporter->id,
            'community_id' => $postA->community_id,
            'reportable_type' => Post::class,
            'reportable_id' => $postA->id,
            'reason' => 'Rule violation',
        ]);

        Report::query()->create([
            'reporter_user_id' => $reporter->id,
            'community_id' => $postB->community_id,
            'reportable_type' => Post::class,
            'reportable_id' => $postB->id,
            'reason' => 'Off-topic',
        ]);

        $response = $this->getJson('/api/mod/queue', $this->headersForUser($moderator));

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame($postA->community_id, $response->json('data.0.community_id'));
    }

    public function test_non_moderator_cannot_access_mod_queue(): void
    {
        $user = User::factory()->create();

        $this->getJson('/api/mod/queue', $this->headersForUser($user))
            ->assertStatus(403)
            ->assertJsonPath('message', 'Moderator access required.');
    }

    public function test_moderator_can_view_moderation_action_history_for_moderated_communities_only(): void
    {
        $moderator = User::factory()->create();
        $ownerA = User::factory()->create();
        $ownerB = User::factory()->create();

        $postA = $this->makePost($ownerA);
        $postB = $this->makePost($ownerB);

        CommunityMembership::query()->create([
            'community_id' => $postA->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        ModerationAction::query()->create([
            'actor_user_id' => $moderator->id,
            'community_id' => $postA->community_id,
            'actionable_type' => Post::class,
            'actionable_id' => $postA->id,
            'action' => 'remove',
        ]);

        ModerationAction::query()->create([
            'actor_user_id' => $ownerB->id,
            'community_id' => $postB->community_id,
            'actionable_type' => Post::class,
            'actionable_id' => $postB->id,
            'action' => 'remove',
        ]);

        $response = $this->getJson('/api/mod/actions', $this->headersForUser($moderator));

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame($postA->community_id, $response->json('data.0.community_id'));
    }

    public function test_moderation_action_history_supports_action_and_target_type_filters(): void
    {
        $moderator = User::factory()->create();
        $owner = User::factory()->create();

        $post = $this->makePost($owner);
        $comment = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $owner->id,
            'body' => 'Please be civil.',
            'depth' => 0,
        ]);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        ModerationAction::query()->create([
            'actor_user_id' => $moderator->id,
            'community_id' => $post->community_id,
            'actionable_type' => Post::class,
            'actionable_id' => $post->id,
            'action' => 'remove',
        ]);

        ModerationAction::query()->create([
            'actor_user_id' => $moderator->id,
            'community_id' => $post->community_id,
            'actionable_type' => Comment::class,
            'actionable_id' => $comment->id,
            'action' => 'lock',
        ]);

        $response = $this->getJson('/api/mod/actions?action=lock&target_type=comment', $this->headersForUser($moderator));

        $response->assertOk();
        $this->assertCount(1, $response->json('data'));
        $this->assertSame('lock', $response->json('data.0.action'));
        $this->assertSame(Comment::class, $response->json('data.0.actionable_type'));
    }

    public function test_non_moderator_cannot_access_mod_actions_history(): void
    {
        $user = User::factory()->create();

        $this->getJson('/api/mod/actions', $this->headersForUser($user))
            ->assertStatus(403)
            ->assertJsonPath('message', 'Moderator access required.');
    }

    public function test_moderation_action_remove_post_resolves_report_and_records_audit(): void
    {
        $moderator = User::factory()->create();
        $owner = User::factory()->create();
        $reporter = User::factory()->create();

        $post = $this->makePost($owner);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        $report = Report::query()->create([
            'reporter_user_id' => $reporter->id,
            'community_id' => $post->community_id,
            'reportable_type' => Post::class,
            'reportable_id' => $post->id,
            'reason' => 'Spam',
            'status' => 'open',
        ]);

        $this->postJson('/api/mod/actions', [
            'target_type' => 'post',
            'target_id' => $post->id,
            'action' => 'remove',
            'reason' => 'Confirmed spam links.',
            'report_id' => $report->id,
        ], $this->headersForUser($moderator))
            ->assertOk()
            ->assertJsonPath('data.target.is_removed', true)
            ->assertJsonPath('data.report_status', 'resolved');

        $this->assertDatabaseHas('posts', [
            'id' => $post->id,
            'is_removed' => true,
        ]);

        $this->assertDatabaseHas('reports', [
            'id' => $report->id,
            'status' => 'resolved',
            'reviewed_by_user_id' => $moderator->id,
        ]);

        $this->assertDatabaseHas('moderation_actions', [
            'actor_user_id' => $moderator->id,
            'community_id' => $post->community_id,
            'report_id' => $report->id,
            'actionable_type' => Post::class,
            'actionable_id' => $post->id,
            'action' => 'remove',
        ]);

        $this->assertSame(1, ModerationAction::query()->count());
    }

    public function test_moderation_action_dispatches_async_follow_up_job(): void
    {
        Queue::fake();

        $moderator = User::factory()->create();
        $owner = User::factory()->create();

        $post = $this->makePost($owner);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        $this->postJson('/api/mod/actions', [
            'target_type' => 'post',
            'target_id' => $post->id,
            'action' => 'lock',
            'reason' => 'Escalated review.',
        ], $this->headersForUser($moderator))->assertOk();

        Queue::assertPushed(ProcessModerationActionJob::class, 1);
    }

    public function test_moderation_action_can_lock_comment_without_report(): void
    {
        $moderator = User::factory()->create();
        $owner = User::factory()->create();

        $post = $this->makePost($owner);
        $comment = Comment::query()->create([
            'post_id' => $post->id,
            'author_user_id' => $owner->id,
            'body' => 'Please be civil.',
            'depth' => 0,
        ]);

        CommunityMembership::query()->create([
            'community_id' => $post->community_id,
            'user_id' => $moderator->id,
            'role' => 'moderator',
        ]);

        $this->postJson('/api/mod/actions', [
            'target_type' => 'comment',
            'target_id' => $comment->id,
            'action' => 'lock',
            'reason' => 'Thread is getting heated.',
        ], $this->headersForUser($moderator))
            ->assertOk()
            ->assertJsonPath('data.target.is_locked', true);

        $this->assertDatabaseHas('comments', [
            'id' => $comment->id,
            'is_locked' => true,
        ]);
    }

    private function makePost(User $owner): Post
    {
        $community = Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'Community '.Str::random(6),
            'slug' => 'community-'.Str::lower(Str::random(6)),
            'is_private' => false,
        ]);

        CommunityMembership::query()->create([
            'community_id' => $community->id,
            'user_id' => $owner->id,
            'role' => 'owner',
        ]);

        return Post::query()->create([
            'community_id' => $community->id,
            'author_user_id' => $owner->id,
            'title' => 'Moderation target',
            'slug' => 'moderation-target-'.Str::lower(Str::random(5)),
            'body' => 'Body',
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
