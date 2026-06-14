<?php

namespace Database\Seeders;

use App\Models\Comment;
use App\Models\Community;
use App\Models\CommunityMembership;
use App\Models\Post;
use App\Models\User;
use App\Models\Vote;
use Illuminate\Database\Seeder;
use Illuminate\Support\Facades\Hash;

class BaselineDataSeeder extends Seeder
{
    /**
     * Seed baseline forum data for local development.
     */
    public function run(): void
    {
        $admin = User::query()->firstOrCreate(
            ['email' => 'admin@mifolyo.local'],
            [
                'name' => 'MiFolyo Admin',
                'username' => 'mifolyo_admin',
                'password' => Hash::make('password123'),
                'email_verified_at' => now(),
            ]
        );

        $moderator = User::query()->firstOrCreate(
            ['email' => 'moderator@mifolyo.local'],
            [
                'name' => 'MiFolyo Moderator',
                'username' => 'mifolyo_mod',
                'password' => Hash::make('password123'),
                'email_verified_at' => now(),
            ]
        );

        $member = User::query()->firstOrCreate(
            ['email' => 'member@mifolyo.local'],
            [
                'name' => 'MiFolyo Member',
                'username' => 'mifolyo_member',
                'password' => Hash::make('password123'),
                'email_verified_at' => now(),
            ]
        );

        $admin->forceFill(['username' => $admin->username ?? 'mifolyo_admin'])->save();
        $moderator->forceFill(['username' => $moderator->username ?? 'mifolyo_mod'])->save();
        $member->forceFill(['username' => $member->username ?? 'mifolyo_member'])->save();

        $general = Community::query()->firstOrCreate(
            ['slug' => 'general'],
            [
                'owner_user_id' => $admin->id,
                'name' => 'General',
                'description' => 'General discussion for MiFolyo users.',
                'is_private' => false,
                'is_nsfw' => false,
            ]
        );

        $announcements = Community::query()->firstOrCreate(
            ['slug' => 'announcements'],
            [
                'owner_user_id' => $admin->id,
                'name' => 'Announcements',
                'description' => 'Official updates and release notes.',
                'is_private' => false,
                'is_nsfw' => false,
            ]
        );

        $this->upsertMembership($general->id, $admin->id, 'owner');
        $this->upsertMembership($general->id, $moderator->id, 'moderator');
        $this->upsertMembership($general->id, $member->id, 'member');
        $this->upsertMembership($announcements->id, $admin->id, 'owner');

        $welcomePost = Post::query()->firstOrCreate(
            [
                'community_id' => $general->id,
                'slug' => 'welcome-to-mifolyo',
            ],
            [
                'author_user_id' => $admin->id,
                'title' => 'Welcome to MiFolyo',
                'body' => 'Introduce yourself and share what you are building.',
                'content_type' => 'text',
                'published_at' => now()->subDay(),
            ]
        );

        $releasePost = Post::query()->firstOrCreate(
            [
                'community_id' => $announcements->id,
                'slug' => 'forum-engine-alpha',
            ],
            [
                'author_user_id' => $admin->id,
                'title' => 'Forum Engine Alpha',
                'body' => 'Core community, posts, comments, and moderation are now available.',
                'content_type' => 'text',
                'published_at' => now()->subHours(8),
            ]
        );

        $welcomeComment = Comment::query()->firstOrCreate(
            [
                'post_id' => $welcomePost->id,
                'author_user_id' => $member->id,
                'body' => 'Excited to be here. Building my first community app.',
            ],
            [
                'depth' => 0,
            ]
        );

        Comment::query()->firstOrCreate(
            [
                'post_id' => $welcomePost->id,
                'author_user_id' => $moderator->id,
                'body' => 'Welcome aboard. Please review the posting guidelines.',
            ],
            [
                'parent_comment_id' => $welcomeComment->id,
                'depth' => 1,
            ]
        );

        Vote::query()->firstOrCreate(
            [
                'user_id' => $member->id,
                'votable_type' => Post::class,
                'votable_id' => $releasePost->id,
            ],
            [
                'value' => 1,
            ]
        );

        $this->refreshCommunityCounters($general);
        $this->refreshCommunityCounters($announcements);
        $this->refreshPostCounters($welcomePost);
        $this->refreshPostCounters($releasePost);
    }

    private function upsertMembership(int $communityId, int $userId, string $role): void
    {
        CommunityMembership::query()->updateOrCreate(
            [
                'community_id' => $communityId,
                'user_id' => $userId,
            ],
            [
                'role' => $role,
                'is_muted' => false,
            ]
        );
    }

    private function refreshCommunityCounters(Community $community): void
    {
        $community->forceFill([
            'member_count' => CommunityMembership::query()->where('community_id', $community->id)->count(),
            'post_count' => Post::query()->where('community_id', $community->id)->count(),
            'last_posted_at' => Post::query()->where('community_id', $community->id)->max('published_at'),
        ])->save();
    }

    private function refreshPostCounters(Post $post): void
    {
        $comments = Comment::query()->where('post_id', $post->id);
        $votes = Vote::query()->where('votable_type', Post::class)->where('votable_id', $post->id);

        $upvotes = (clone $votes)->where('value', 1)->count();
        $downvotes = (clone $votes)->where('value', -1)->count();

        $post->forceFill([
            'comment_count' => $comments->count(),
            'upvote_count' => $upvotes,
            'downvote_count' => $downvotes,
            'score' => $upvotes - $downvotes,
        ])->save();
    }
}
