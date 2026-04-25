<?php

namespace App\Services;

use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use Illuminate\Contracts\Pagination\LengthAwarePaginator;
use Illuminate\Database\Eloquent\Builder;

class FeedService
{
    public function homeFeed(?User $user, string $sort, int $perPage): LengthAwarePaginator
    {
        $query = Post::query()
            ->with(['community', 'author'])
            ->where('is_removed', false)
            ->where(function (Builder $query) use ($user): void {
                $query->whereHas('community', function (Builder $communityQuery): void {
                    $communityQuery->where('is_private', false);
                });

                if ($user) {
                    $query->orWhereHas('community.memberships', function (Builder $membershipQuery) use ($user): void {
                        $membershipQuery->where('user_id', $user->id);
                    });
                }
            });

        $this->applySort($query, $sort);

        return $query->paginate($perPage);
    }

    public function communityFeed(Community $community, string $sort, int $perPage): LengthAwarePaginator
    {
        $query = Post::query()
            ->with(['community', 'author'])
            ->where('community_id', $community->id)
            ->where('is_removed', false);

        $this->applySort($query, $sort);

        return $query->paginate($perPage);
    }

    public function canViewCommunityFeed(Community $community, ?User $user): bool
    {
        if (! $community->is_private) {
            return true;
        }

        if (! $user) {
            return false;
        }

        return $community->memberships()->where('user_id', $user->id)->exists();
    }

    private function applySort(Builder $query, string $sort): void
    {
        match ($sort) {
            'new' => $query
                ->orderByDesc('published_at')
                ->orderByDesc('id'),
            'top' => $query
                ->orderByDesc('score')
                ->orderByDesc('comment_count')
                ->orderByDesc('published_at')
                ->orderByDesc('id'),
            default => $query
                ->orderByRaw('(score * 2 + comment_count) DESC')
                ->orderByDesc('published_at')
                ->orderByDesc('id'),
        };
    }
}
