<?php

namespace App\Http\Controllers;

use App\Http\Requests\FeedQueryRequest;
use App\Models\Community;
use App\Services\FeedService;
use Illuminate\Http\JsonResponse;

class FeedController extends Controller
{
    public function __construct(private readonly FeedService $feedService) {}

    public function home(FeedQueryRequest $request): JsonResponse
    {
        [$sort, $perPage] = $this->validatedFeedParams($request->validated());

        $feed = $this->feedService->homeFeed($request->user('api'), $sort, $perPage);

        return response()->json($feed);
    }

    public function community(FeedQueryRequest $request, Community $community): JsonResponse
    {
        if (! $this->feedService->canViewCommunityFeed($community, $request->user('api'))) {
            return response()->json([
                'message' => 'This community is private.',
            ], 403);
        }

        [$sort, $perPage] = $this->validatedFeedParams($request->validated());
        $feed = $this->feedService->communityFeed($community, $sort, $perPage);

        return response()->json($feed);
    }

    private function validatedFeedParams(array $validated): array
    {
        return [
            $validated['sort'] ?? 'hot',
            (int) ($validated['per_page'] ?? 20),
        ];
    }
}
