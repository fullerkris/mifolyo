<?php

namespace App\Http\Controllers;

use App\Http\Requests\ModerationActionRequest;
use App\Http\Requests\ModerationHistoryRequest;
use App\Http\Requests\ModerationQueueRequest;
use App\Jobs\ProcessModerationActionJob;
use App\Models\Comment;
use App\Models\CommunityMembership;
use App\Models\ModerationAction;
use App\Models\Post;
use App\Models\Report;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Http\JsonResponse;
use Illuminate\Support\Facades\DB;

class ModerationController extends Controller
{
    public function actions(ModerationHistoryRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $communityIds = $this->moderatedCommunityIds((int) $request->user()->id);

        if ($communityIds === []) {
            return response()->json([
                'message' => 'Moderator access required.',
            ], 403);
        }

        if (! empty($validated['community_id']) && ! in_array((int) $validated['community_id'], $communityIds, true)) {
            return response()->json([
                'message' => 'Moderator access required for this community.',
            ], 403);
        }

        $query = ModerationAction::query()
            ->whereIn('community_id', $communityIds)
            ->with(['actor', 'community', 'report', 'actionable'])
            ->orderByDesc('created_at');

        if (! empty($validated['community_id'])) {
            $query->where('community_id', (int) $validated['community_id']);
        }

        if (! empty($validated['action'])) {
            $query->where('action', $validated['action']);
        }

        if (! empty($validated['target_type'])) {
            $targetClass = $validated['target_type'] === 'post' ? Post::class : Comment::class;
            $query->where('actionable_type', $targetClass);
        }

        return response()->json($query->paginate((int) ($validated['per_page'] ?? 20)));
    }

    public function queue(ModerationQueueRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $communityIds = $this->moderatedCommunityIds((int) $request->user()->id);

        if ($communityIds === []) {
            return response()->json([
                'message' => 'Moderator access required.',
            ], 403);
        }

        $query = Report::query()
            ->whereIn('community_id', $communityIds)
            ->with(['reporter', 'community', 'reportable'])
            ->orderByDesc('created_at');

        if (! empty($validated['status'])) {
            $query->where('status', $validated['status']);
        } else {
            $query->where('status', 'open');
        }

        return response()->json($query->paginate((int) ($validated['per_page'] ?? 20)));
    }

    public function action(ModerationActionRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $target = $this->resolveTarget($validated['target_type'], (int) $validated['target_id']);

        if (! $target) {
            return response()->json([
                'message' => 'Moderation target not found.',
            ], 404);
        }

        $communityId = $target instanceof Post ? (int) $target->community_id : (int) $target->post->community_id;
        $actorUserId = (int) $request->user()->id;

        if (! $this->isModeratorForCommunity($actorUserId, $communityId)) {
            return response()->json([
                'message' => 'Moderator access required for this community.',
            ], 403);
        }

        $report = null;

        if (! empty($validated['report_id'])) {
            $report = Report::query()->findOrFail((int) $validated['report_id']);

            $targetTypeClass = $target::class;
            $targetId = (int) $target->getKey();

            if (
                (int) $report->community_id !== $communityId
                || $report->reportable_type !== $targetTypeClass
                || (int) $report->reportable_id !== $targetId
            ) {
                return response()->json([
                    'message' => 'Report does not match moderation target.',
                ], 422);
            }
        }

        $result = DB::transaction(function () use ($target, $validated, $actorUserId, $communityId, $report): array {
            $currentState = [
                'is_removed' => (bool) $target->is_removed,
                'is_locked' => (bool) $target->is_locked,
            ];

            if ($validated['action'] === 'remove') {
                $target->is_removed = true;
            }

            if ($validated['action'] === 'lock') {
                $target->is_locked = true;
            }

            $target->save();

            if ($report) {
                $report->forceFill([
                    'status' => 'resolved',
                    'reviewed_by_user_id' => $actorUserId,
                    'reviewed_at' => now(),
                ])->save();
            }

            $action = ModerationAction::query()->create([
                'actor_user_id' => $actorUserId,
                'community_id' => $communityId,
                'report_id' => $report?->id,
                'actionable_type' => $target::class,
                'actionable_id' => $target->id,
                'action' => $validated['action'],
                'reason' => $validated['reason'] ?? null,
                'metadata' => [
                    'previous' => $currentState,
                    'current' => [
                        'is_removed' => (bool) $target->is_removed,
                        'is_locked' => (bool) $target->is_locked,
                    ],
                ],
            ]);

            return [
                'action' => $action,
                'target' => $target,
                'report' => $report,
            ];
        });

        ProcessModerationActionJob::dispatch((int) $result['action']->id);

        return response()->json([
            'data' => [
                'moderation_action' => $result['action']->load(['actor', 'report']),
                'target' => [
                    'id' => $result['target']->id,
                    'type' => $validated['target_type'],
                    'is_removed' => (bool) $result['target']->is_removed,
                    'is_locked' => (bool) $result['target']->is_locked,
                ],
                'report_status' => $result['report']?->status,
            ],
        ]);
    }

    private function moderatedCommunityIds(int $userId): array
    {
        return CommunityMembership::query()
            ->where('user_id', $userId)
            ->whereIn('role', ['moderator', 'owner'])
            ->pluck('community_id')
            ->all();
    }

    private function isModeratorForCommunity(int $userId, int $communityId): bool
    {
        return CommunityMembership::query()
            ->where('community_id', $communityId)
            ->where('user_id', $userId)
            ->whereIn('role', ['moderator', 'owner'])
            ->exists();
    }

    private function resolveTarget(string $type, int $id): ?Model
    {
        return match ($type) {
            'post' => Post::query()->find($id),
            'comment' => Comment::query()->with('post')->find($id),
            default => null,
        };
    }
}
