<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreCommunityRequest;
use App\Models\Community;
use App\Models\CommunityMembership;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Str;

class CommunityController extends Controller
{
    public function index(Request $request): JsonResponse
    {
        $perPage = min(max((int) $request->integer('per_page', 20), 1), 100);

        $communities = Community::query()
            ->where('is_private', false)
            ->orderByDesc('member_count')
            ->orderByDesc('created_at')
            ->paginate($perPage);

        return response()->json($communities);
    }

    public function store(StoreCommunityRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $user = $request->user();

        $community = DB::transaction(function () use ($validated, $user) {
            $slug = $this->generateUniqueSlug($validated['name']);

            $community = Community::query()->create([
                'owner_user_id' => $user->id,
                'name' => $validated['name'],
                'slug' => $slug,
                'description' => $validated['description'] ?? null,
                'is_private' => $validated['is_private'] ?? false,
                'is_nsfw' => $validated['is_nsfw'] ?? false,
                'member_count' => 1,
            ]);

            CommunityMembership::query()->create([
                'community_id' => $community->id,
                'user_id' => $user->id,
                'role' => 'owner',
            ]);

            return $community;
        });

        return response()->json([
            'data' => $community->load('owner'),
        ], 201);
    }

    public function join(Request $request, Community $community): JsonResponse
    {
        if ($community->is_private) {
            return response()->json([
                'message' => 'This community is private.',
            ], 403);
        }

        $user = $request->user();

        [$membership, $created] = DB::transaction(function () use ($community, $user) {
            $membership = CommunityMembership::query()->firstOrCreate(
                [
                    'community_id' => $community->id,
                    'user_id' => $user->id,
                ],
                [
                    'role' => 'member',
                    'is_muted' => false,
                ]
            );

            if ($membership->wasRecentlyCreated) {
                Community::query()->whereKey($community->id)->increment('member_count');
            }

            return [$membership, $membership->wasRecentlyCreated];
        });

        return response()->json([
            'data' => [
                'joined' => $created,
                'membership' => $membership,
            ],
        ]);
    }

    private function generateUniqueSlug(string $name): string
    {
        $baseSlug = Str::slug($name);

        if ($baseSlug === '') {
            $baseSlug = 'community';
        }

        $candidate = $baseSlug;
        $suffix = 2;

        while (Community::query()->where('slug', $candidate)->exists()) {
            $candidate = $baseSlug.'-'.$suffix;
            $suffix++;
        }

        return $candidate;
    }
}
