<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreVoteRequest;
use App\Models\Comment;
use App\Models\Post;
use App\Models\Vote;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class VoteController extends Controller
{
    public function store(StoreVoteRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $votable = $this->resolveVotable($validated['votable_type'], (int) $validated['votable_id']);

        if (! $votable) {
            return response()->json([
                'message' => 'Votable resource not found.',
            ], 404);
        }

        $post = $votable instanceof Post ? $votable : $votable->post;

        if (! $this->canAccessPost($request, $post)) {
            return response()->json([
                'message' => 'You do not have access to vote on this resource.',
            ], 403);
        }

        if ($post->is_locked || $post->is_removed) {
            return response()->json([
                'message' => 'Voting is disabled for this resource.',
            ], 423);
        }

        $votableType = $votable::class;
        $votableId = (int) $votable->getKey();
        $value = (int) $validated['value'];
        $userId = (int) $request->user()->id;

        $result = DB::transaction(function () use ($votable, $votableType, $votableId, $value, $userId): array {
            $lockedVotable = $votable::query()->lockForUpdate()->findOrFail($votableId);

            $existingVote = Vote::query()
                ->where('user_id', $userId)
                ->where('votable_type', $votableType)
                ->where('votable_id', $votableId)
                ->lockForUpdate()
                ->first();

            if ($existingVote && (int) $existingVote->value === $value) {
                return [
                    'vote' => $existingVote,
                    'changed' => false,
                    'votable' => $lockedVotable,
                ];
            }

            $oldValue = $existingVote ? (int) $existingVote->value : 0;

            if ($existingVote) {
                $existingVote->value = $value;
                $existingVote->save();
                $vote = $existingVote;
            } else {
                $vote = Vote::query()->create([
                    'user_id' => $userId,
                    'votable_type' => $votableType,
                    'votable_id' => $votableId,
                    'value' => $value,
                ]);
            }

            $deltaScore = $value - $oldValue;
            $deltaUpvotes = ($value === 1 ? 1 : 0) - ($oldValue === 1 ? 1 : 0);
            $deltaDownvotes = ($value === -1 ? 1 : 0) - ($oldValue === -1 ? 1 : 0);

            $votable::query()->whereKey($votableId)->update([
                'score' => DB::raw('score + '.$deltaScore),
                'upvote_count' => DB::raw('upvote_count + '.$deltaUpvotes),
                'downvote_count' => DB::raw('downvote_count + '.$deltaDownvotes),
            ]);

            $lockedVotable->refresh();

            return [
                'vote' => $vote,
                'changed' => true,
                'votable' => $lockedVotable,
            ];
        });

        return response()->json([
            'data' => [
                'changed' => $result['changed'],
                'vote' => $result['vote'],
                'votable' => [
                    'type' => $validated['votable_type'],
                    'id' => $result['votable']->id,
                    'score' => $result['votable']->score,
                    'upvote_count' => $result['votable']->upvote_count,
                    'downvote_count' => $result['votable']->downvote_count,
                ],
            ],
        ]);
    }

    private function resolveVotable(string $type, int $id): ?Model
    {
        return match ($type) {
            'post' => Post::query()->with('community')->find($id),
            'comment' => Comment::query()->with('post.community')->find($id),
            default => null,
        };
    }

    private function canAccessPost(Request $request, Post $post): bool
    {
        if (! $post->community || ! $post->community->is_private) {
            return true;
        }

        $user = $request->user();

        if (! $user) {
            return false;
        }

        return $post->community->memberships()->where('user_id', $user->id)->exists();
    }
}
