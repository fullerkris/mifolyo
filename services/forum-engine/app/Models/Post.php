<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Database\Eloquent\Relations\MorphMany;

#[Fillable([
    'community_id',
    'author_user_id',
    'title',
    'slug',
    'body',
    'url',
    'source_url',
    'source_url_hash',
    'source_domain',
    'source_path',
    'content_type',
    'score',
    'comment_count',
    'upvote_count',
    'downvote_count',
    'is_locked',
    'is_removed',
    'is_nsfw',
    'published_at',
])]
class Post extends Model
{
    use HasFactory;

    protected function casts(): array
    {
        return [
            'is_locked' => 'boolean',
            'is_removed' => 'boolean',
            'is_nsfw' => 'boolean',
            'published_at' => 'datetime',
        ];
    }

    public function community(): BelongsTo
    {
        return $this->belongsTo(Community::class);
    }

    public function author(): BelongsTo
    {
        return $this->belongsTo(User::class, 'author_user_id');
    }

    public function comments(): HasMany
    {
        return $this->hasMany(Comment::class);
    }

    public function votes(): MorphMany
    {
        return $this->morphMany(Vote::class, 'votable');
    }

    public function reports(): MorphMany
    {
        return $this->morphMany(Report::class, 'reportable');
    }

    public function moderationActions(): MorphMany
    {
        return $this->morphMany(ModerationAction::class, 'actionable');
    }
}
