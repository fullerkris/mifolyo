<?php

namespace App\Models;

// use Illuminate\Contracts\Auth\MustVerifyEmail;
use Database\Factories\UserFactory;
use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Attributes\Hidden;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Foundation\Auth\User as Authenticatable;
use Illuminate\Notifications\Notifiable;

#[Fillable(['name', 'username', 'email', 'password', 'level', 'standing'])]
#[Hidden(['password', 'remember_token', 'api_token', 'onboarding_started_at', 'onboarding_welcome_at', 'onboarding_discuss_at', 'onboarding_vote_at'])]
class User extends Authenticatable
{
    /** @use HasFactory<UserFactory> */
    use HasFactory, Notifiable;

    /**
     * Get the attributes that should be cast.
     *
     * @return array<string, string>
     */
    protected function casts(): array
    {
        return [
            'api_token_expires_at' => 'datetime',
            'api_token_last_used_at' => 'datetime',
            'email_verified_at' => 'datetime',
            'level' => 'integer',
            'password' => 'hashed',
            'onboarding_started_at' => 'immutable_datetime',
            'onboarding_welcome_at' => 'immutable_datetime',
            'onboarding_discuss_at' => 'immutable_datetime',
            'onboarding_vote_at' => 'immutable_datetime',
        ];
    }

    public function onboardingState(): array
    {
        $started = $this->onboarding_started_at;
        $enrolled = $started !== null && $started->lessThanOrEqualTo(now());
        $firstWeek = $enrolled && now()->lessThan($started->addDays(7));

        return [
            'welcome' => $enrolled && $this->onboarding_welcome_at === null,
            'discuss' => $firstWeek && $this->onboarding_discuss_at === null,
            'vote' => $firstWeek && $this->onboarding_vote_at === null,
        ];
    }

    public function ownedCommunities(): HasMany
    {
        return $this->hasMany(Community::class, 'owner_user_id');
    }

    public function memberships(): HasMany
    {
        return $this->hasMany(CommunityMembership::class);
    }

    public function posts(): HasMany
    {
        return $this->hasMany(Post::class, 'author_user_id');
    }

    public function comments(): HasMany
    {
        return $this->hasMany(Comment::class, 'author_user_id');
    }

    public function votes(): HasMany
    {
        return $this->hasMany(Vote::class);
    }

    public function reports(): HasMany
    {
        return $this->hasMany(Report::class, 'reporter_user_id');
    }

    public function reviewedReports(): HasMany
    {
        return $this->hasMany(Report::class, 'reviewed_by_user_id');
    }

    public function moderationActions(): HasMany
    {
        return $this->hasMany(ModerationAction::class, 'actor_user_id');
    }
}
