<?php

namespace App\Http\Controllers;

use App\Http\Requests\StorePostRequest;
use App\Models\Community;
use App\Models\Post;
use Illuminate\Http\JsonResponse;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class PostController extends Controller
{
    public function show(Post $post): JsonResponse
    {
        $post->load(['community', 'author']);

        return response()->json([
            'data' => $post,
        ]);
    }

    public function store(StorePostRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $community = Community::query()->where('slug', $validated['community_slug'])->firstOrFail();
        $user = $request->user();

        if ($community->is_private) {
            $isMember = $community->memberships()->where('user_id', $user->id)->exists();
            if (! $isMember) {
                return response()->json([
                    'message' => 'You must be a member to post in this private community.',
                ], 403);
            }
        }

        $contentType = $validated['content_type'] ?? 'text';

        $post = DB::transaction(function () use ($validated, $community, $user, $contentType) {
            $slug = $this->generateUniqueSlug($community->id, $validated['title']);

            $post = Post::query()->create([
                'community_id' => $community->id,
                'author_user_id' => $user->id,
                'title' => $validated['title'],
                'slug' => $slug,
                'body' => $validated['body'] ?? null,
                'url' => $validated['url'] ?? null,
                'content_type' => $contentType,
                'is_nsfw' => $validated['is_nsfw'] ?? false,
                'published_at' => now(),
            ]);

            Community::query()->whereKey($community->id)->update([
                'post_count' => DB::raw('post_count + 1'),
                'last_posted_at' => now(),
            ]);

            return $post;
        });

        $post->load(['community', 'author']);

        return response()->json([
            'data' => $post,
        ], 201);
    }

    private function generateUniqueSlug(int $communityId, string $title): string
    {
        $baseSlug = Str::slug($title);

        if ($baseSlug === '') {
            $baseSlug = 'post';
        }

        $candidate = $baseSlug;
        $suffix = 2;

        while (Post::query()->where('community_id', $communityId)->where('slug', $candidate)->exists()) {
            $candidate = $baseSlug.'-'.$suffix;
            $suffix++;
        }

        return $candidate;
    }
}
