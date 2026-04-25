<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;

#[Fillable([
    'owner_user_id',
    'name',
    'slug',
    'description',
    'is_private',
    'is_nsfw',
    'member_count',
    'post_count',
    'last_posted_at',
])]
class Community extends Model
{
    use HasFactory;

    protected function casts(): array
    {
        return [
            'is_private' => 'boolean',
            'is_nsfw' => 'boolean',
            'last_posted_at' => 'datetime',
        ];
    }

    public function owner(): BelongsTo
    {
        return $this->belongsTo(User::class, 'owner_user_id');
    }

    public function memberships(): HasMany
    {
        return $this->hasMany(CommunityMembership::class);
    }

    public function posts(): HasMany
    {
        return $this->hasMany(Post::class);
    }

    public function reports(): HasMany
    {
        return $this->hasMany(Report::class);
    }

    public function moderationActions(): HasMany
    {
        return $this->hasMany(ModerationAction::class);
    }
}
