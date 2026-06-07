<?php

namespace App\Http\Controllers;

use App\Http\Requests\StoreReportRequest;
use App\Jobs\ProcessReportCreatedJob;
use App\Models\Comment;
use App\Models\Post;
use App\Models\Report;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;

class ReportController extends Controller
{
    public function store(StoreReportRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $reportable = $this->resolveReportable($validated['reportable_type'], (int) $validated['reportable_id']);

        if (! $reportable) {
            return response()->json([
                'message' => 'Report target not found.',
            ], 404);
        }

        $post = $reportable instanceof Post ? $reportable : $reportable->post;

        if (! $this->canAccessPost($request, $post)) {
            return response()->json([
                'message' => 'You do not have access to report this resource.',
            ], 403);
        }

        $report = Report::query()->create([
            'reporter_user_id' => $request->user()->id,
            'community_id' => $post->community_id,
            'reportable_type' => $reportable::class,
            'reportable_id' => $reportable->getKey(),
            'reason' => $validated['reason'],
            'details' => $validated['details'] ?? null,
            'status' => 'open',
        ]);

        ProcessReportCreatedJob::dispatch((int) $report->id);

        return response()->json([
            'data' => $report->load(['reporter', 'community']),
        ], 201);
    }

    private function resolveReportable(string $type, int $id): ?Model
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
