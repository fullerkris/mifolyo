<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\HasMany;
use Illuminate\Database\Eloquent\Relations\MorphMany;

#[Fillable([
    'post_id',
    'parent_comment_id',
    'author_user_id',
    'body',
    'depth',
    'score',
    'upvote_count',
    'downvote_count',
    'is_removed',
    'is_locked',
])]
class Comment extends Model
{
    use HasFactory;

    protected function casts(): array
    {
        return [
            'is_removed' => 'boolean',
            'is_locked' => 'boolean',
        ];
    }

    public function post(): BelongsTo
    {
        return $this->belongsTo(Post::class);
    }

    public function author(): BelongsTo
    {
        return $this->belongsTo(User::class, 'author_user_id');
    }

    public function parent(): BelongsTo
    {
        return $this->belongsTo(Comment::class, 'parent_comment_id');
    }

    public function children(): HasMany
    {
        return $this->hasMany(Comment::class, 'parent_comment_id');
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
