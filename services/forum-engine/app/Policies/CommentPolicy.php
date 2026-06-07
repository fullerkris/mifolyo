<?php

namespace App\Policies;

use App\Models\Comment;
use App\Models\CommunityMembership;
use App\Models\User;

class CommentPolicy
{
    public function update(User $user, Comment $comment): bool
    {
        return $this->isAuthor($user, $comment) || $this->isModeratorOrOwner($user, $this->communityId($comment));
    }

    public function delete(User $user, Comment $comment): bool
    {
        return $this->isAuthor($user, $comment) || $this->isModeratorOrOwner($user, $this->communityId($comment));
    }

    public function remove(User $user, Comment $comment): bool
    {
        return $this->isAuthor($user, $comment) || $this->isModeratorOrOwner($user, $this->communityId($comment));
    }

    public function lock(User $user, Comment $comment): bool
    {
        return $this->isModeratorOrOwner($user, $this->communityId($comment));
    }

    private function isAuthor(User $user, Comment $comment): bool
    {
        return $comment->author_user_id === $user->id;
    }

    private function communityId(Comment $comment): int
    {
        if ($comment->relationLoaded('post') && $comment->post) {
            return (int) $comment->post->community_id;
        }

        return (int) $comment->post()->value('community_id');
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
