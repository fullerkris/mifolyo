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

        $votableType = $votable::class;
        $votableId = (int) $votable->getKey();
        $value = (int) $validated['value'];
        $userId = (int) $request->user()->id;

        $result = DB::transaction(function () use ($request, $validated, $votable, $votableType, $votableId, $value, $userId): array {
            // Lock the parent first for both post and comment votes, then recheck access/state.
            $post = Post::query()->with('community')->lockForUpdate()
                ->findOrFail($votable instanceof Post ? $votableId : $votable->post_id);
            $lockedVotable = $votable instanceof Post ? $post : Comment::query()->lockForUpdate()->findOrFail($votableId);
            abort_unless($this->canAccessPost($request, $post), 403, 'You do not have access to vote on this resource.');
            abort_if(isset($validated['post_id']) && (int) $validated['post_id'] !== $post->id, 422, 'This resource does not belong to the selected thread.');
            abort_if($post->is_locked || $post->is_removed || $lockedVotable->is_locked || $lockedVotable->is_removed, 423, 'Voting is disabled for this resource.');

            $existingVote = Vote::query()
                ->where('user_id', $userId)
                ->where('votable_type', $votableType)
                ->where('votable_id', $votableId)
                ->lockForUpdate()
                ->first();

            $oldValue = $existingVote ? (int) $existingVote->value : 0;
            if ($oldValue === $value) {
                return [
                    'vote' => $existingVote,
                    'changed' => false,
                    'votable' => $lockedVotable,
                ];
            }

            if ($value === 0) {
                $existingVote?->delete();
                $vote = null;
            } elseif ($existingVote) {
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
                'viewer_vote' => $value,
                'vote' => $result['vote'],
                'votable' => [
                    'type' => $validated['votable_type'],
                    'id' => $result['votable']->id,
                    'score' => $result['votable']->score,
                    'upvote_count' => $result['votable']->upvote_count,
                    'downvote_count' => $result['votable']->downvote_count,
                ],
            ],
        ])->header('Cache-Control', 'private, no-store');
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
