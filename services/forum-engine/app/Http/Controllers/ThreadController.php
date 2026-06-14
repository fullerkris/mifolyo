<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreThreadRequest;
use App\Http\Requests\ThreadsByUrlRequest;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use App\Support\SourceUrlNormalizer;
use Illuminate\Database\Eloquent\Builder;
use Illuminate\Http\JsonResponse;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class ThreadController extends Controller
{
    public function store(StoreThreadRequest $request): JsonResponse
    {
        $validated = $request->validated();
        $community = Community::query()->where('slug', $validated['community_slug'])->firstOrFail();
        $user = $request->user();

        if (! $this->canCreateInCommunity($community, $user)) {
            return response()->json([
                'message' => 'You must be a member to create a thread in this private community.',
            ], 403);
        }

        $source = SourceUrlNormalizer::normalize($validated['source_url']);

        $thread = DB::transaction(function () use ($validated, $community, $user, $source): Post {
            $thread = Post::query()->create(array_merge([
                'community_id' => $community->id,
                'author_user_id' => $user->id,
                'title' => $validated['title'],
                'slug' => $this->generateUniqueSlug($community->id, $validated['title']),
                'body' => $validated['body'] ?? null,
                'url' => $source['source_url'],
                'content_type' => 'link',
                'is_nsfw' => $validated['is_nsfw'] ?? false,
                'published_at' => now(),
            ], $source));

            Community::query()->whereKey($community->id)->update([
                'post_count' => DB::raw('post_count + 1'),
                'last_posted_at' => now(),
            ]);

            return $thread;
        });

        $thread->load(['author', 'community']);

        return response()->json([
            'data' => $this->serializeThread($thread),
        ], 201);
    }

    public function byUrl(ThreadsByUrlRequest $request): JsonResponse
    {
        $validated = $request->validated();
        $source = SourceUrlNormalizer::normalize($validated['url']);
        $sort = $validated['sort'] ?? 'top';
        $perPage = (int) ($validated['per_page'] ?? 20);
        $user = $request->user('api');

        $query = Post::query()
            ->with(['author', 'community'])
            ->where('source_url_hash', $source['source_url_hash'])
            ->where('is_removed', false)
            ->where(function (Builder $query) use ($user): void {
                $query->whereHas('community', function (Builder $communityQuery): void {
                    $communityQuery->where('is_private', false);
                });

                if ($user instanceof User) {
                    $query->orWhereHas('community.memberships', function (Builder $membershipQuery) use ($user): void {
                        $membershipQuery->where('user_id', $user->id);
                    });
                }
            });

        if ($sort === 'new') {
            $query->orderByDesc('published_at')->orderByDesc('id');
        } else {
            $query
                ->orderByDesc('upvote_count')
                ->orderByDesc('score')
                ->orderByDesc('comment_count')
                ->orderByDesc('published_at')
                ->orderByDesc('id');
        }

        $threads = $query->paginate($perPage);

        return response()->json([
            'data' => $threads->getCollection()->map(fn (Post $post): array => $this->serializeThread($post))->values(),
            'meta' => [
                'source_url' => $source['source_url'],
                'source_url_hash' => $source['source_url_hash'],
                'source_domain' => $source['source_domain'],
                'source_path' => $source['source_path'],
                'sort' => $sort,
                'total' => $threads->total(),
                'per_page' => $threads->perPage(),
                'current_page' => $threads->currentPage(),
                'last_page' => $threads->lastPage(),
            ],
        ]);
    }

    private function canCreateInCommunity(Community $community, User $user): bool
    {
        if (! $community->is_private) {
            return true;
        }

        return $community->memberships()->where('user_id', $user->id)->exists();
    }

    private function generateUniqueSlug(int $communityId, string $title): string
    {
        $baseSlug = Str::slug($title);

        if ($baseSlug === '') {
            $baseSlug = 'thread';
        }

        $candidate = $baseSlug;
        $suffix = 2;

        while (Post::query()->where('community_id', $communityId)->where('slug', $candidate)->exists()) {
            $candidate = $baseSlug.'-'.$suffix;
            $suffix++;
        }

        return $candidate;
    }

    private function serializeThread(Post $post): array
    {
        return [
            'id' => $post->id,
            'title' => $post->title,
            'slug' => $post->slug,
            'body' => $post->body,
            'score' => $post->score,
            'upvote_count' => $post->upvote_count,
            'downvote_count' => $post->downvote_count,
            'comment_count' => $post->comment_count,
            'created_at' => $post->created_at?->toISOString(),
            'published_at' => $post->published_at?->toISOString(),
            'source_url' => $post->source_url,
            'source_domain' => $post->source_domain,
            'source_path' => $post->source_path,
            'author' => $post->author ? [
                'id' => $post->author->id,
                'username' => $post->author->username,
                'level' => $post->author->level,
            ] : null,
            'community' => [
                'id' => $post->community->id,
                'name' => $post->community->name,
                'slug' => $post->community->slug,
            ],
        ];
    }
}
