<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreCommentRequest;
use App\Models\Comment;
use App\Models\Post;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class CommentController extends Controller
{
    private const MAX_DEPTH = 8;

    public function index(Request $request, Post $post): JsonResponse
    {
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

        $buildTree = function (string $parentKey) use (&$buildTree, $grouped): array {
            return ($grouped->get($parentKey) ?? collect())
                ->map(function (Comment $comment) use (&$buildTree): array {
                    $node = $comment->toArray();
                    $node['children'] = $buildTree((string) $comment->id);

                    return $node;
                })
                ->values()
                ->all();
        };

        return response()->json([
            'data' => $buildTree('root'),
        ]);
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

        $user = $request->user();

        if (! $user) {
            return false;
        }

        return $community->memberships()->where('user_id', $user->id)->exists();
    }
}
