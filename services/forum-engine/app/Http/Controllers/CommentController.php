<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreCommentRequest;
use App\Models\Comment;
use App\Models\Post;
use App\Models\Vote;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class CommentController extends Controller
{
    private const MAX_DEPTH = 8;

    public function index(Request $request, Post $post): JsonResponse
    {
        abort_if($post->is_removed, 404);
        if (! $this->canAccessPost($request, $post)) {
            return response()->json([
                'message' => 'This post is in a private community.',
            ], 403);
        }

        $comments = Comment::query()
            ->where('post_id', $post->id)
            ->with('author')
            ->orderBy('created_at')
            ->get();

        $grouped = $comments->groupBy(fn (Comment $comment) => (string) ($comment->parent_comment_id ?? 'root'));
        $user = $request->user('api');
        // One viewer-specific lookup for the entire tree, regardless of nesting depth.
        $votes = $user ? Vote::query()->where('user_id', $user->id)->where('votable_type', Comment::class)
            ->whereIn('votable_id', $comments->modelKeys())->pluck('value', 'votable_id') : collect();

        $buildTree = function (string $parentKey) use (&$buildTree, $grouped, $votes): array {
            return ($grouped->get($parentKey) ?? collect())
                ->map(function (Comment $comment) use (&$buildTree, $votes): array {
                    $node = $comment->toArray();
                    $node['author'] = $comment->is_removed ? null : $comment->author?->only(['id', 'username', 'name', 'level']);
                    $node['viewer_vote'] = $comment->is_removed ? 0 : (int) $votes->get($comment->id, 0);
                    if ($comment->is_removed) {
                        $node['body'] = '[Comment removed]';
                    }
                    $node['children'] = $buildTree((string) $comment->id);

                    return $node;
                })
                ->values()
                ->all();
        };

        return response()->json([
            'data' => $buildTree('root'),
        ])->header('Cache-Control', 'private, no-store');
    }

    public function store(StoreCommentRequest $request, Post $post): JsonResponse
    {
        if (! $this->canAccessPost($request, $post)) {
            return response()->json([
                'message' => 'You must be a member to comment in this private community.',
            ], 403);
        }

        if ($post->is_locked) {
            return response()->json([
                'message' => 'This post is locked and cannot receive new comments.',
            ], 423);
        }

        if ($post->is_removed) {
            return response()->json([
                'message' => 'This post has been removed and cannot receive new comments.',
            ], 403);
        }

        $validated = $request->validated();

        $parentComment = null;
        $depth = 0;

        if (! empty($validated['parent_comment_id'])) {
            $parentComment = Comment::query()->findOrFail($validated['parent_comment_id']);

            if ($parentComment->post_id !== $post->id) {
                return response()->json([
                    'message' => 'Parent comment does not belong to this post.',
                ], 422);
            }

            if ($parentComment->is_locked || $parentComment->is_removed) {
                return response()->json(['message' => 'This comment is closed to replies.'], 423);
            }

            $depth = $parentComment->depth + 1;

            if ($depth > self::MAX_DEPTH) {
                return response()->json([
                    'message' => 'Maximum comment depth reached.',
                ], 422);
            }
        }

        $comment = DB::transaction(function () use ($request, $post, $validated, $parentComment, $depth) {
            $comment = Comment::query()->create([
                'post_id' => $post->id,
                'parent_comment_id' => $parentComment?->id,
                'author_user_id' => $request->user()->id,
                'body' => $validated['body'],
                'depth' => $depth,
            ]);

            Post::query()->whereKey($post->id)->increment('comment_count');

            return $comment;
        });

        $comment->load(['author', 'parent']);

        return response()->json([
            'data' => $comment,
        ], 201);
    }

    private function canAccessPost(Request $request, Post $post): bool
    {
        $community = $post->community;

        if (! $community || ! $community->is_private) {
            return true;
        }

        $user = $request->user('api');

        if (! $user) {
            return false;
        }

        return $community->memberships()->where('user_id', $user->id)->exists();
    }
}
