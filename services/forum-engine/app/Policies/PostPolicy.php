<?php

namespace App\Policies;

use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;

class PostPolicy
{
    public function update(User $user, Post $post): bool
    {
        return $this->isAuthor($user, $post) || $this->isModeratorOrOwner($user, $post->community_id);
    }

    public function delete(User $user, Post $post): bool
    {
        return $this->isAuthor($user, $post) || $this->isModeratorOrOwner($user, $post->community_id);
    }

    public function remove(User $user, Post $post): bool
    {
        return $this->isAuthor($user, $post) || $this->isModeratorOrOwner($user, $post->community_id);
    }

    public function lock(User $user, Post $post): bool
    {
        return $this->isModeratorOrOwner($user, $post->community_id);
    }

    private function isAuthor(User $user, Post $post): bool
    {
        return $post->author_user_id === $user->id;
    }

    private function isModeratorOrOwner(User $user, int $communityId): bool
    {
        return CommunityMembership::query()
            ->where('community_id', $communityId)
            ->where('user_id', $user->id)
            ->whereIn('role', ['moderator', 'owner'])
            ->exists();
    }
}
