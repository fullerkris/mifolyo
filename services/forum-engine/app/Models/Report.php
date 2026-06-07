<?php

namespace App\Models;

use Illuminate\Database\Eloquent\Attributes\Fillable;
use Illuminate\Database\Eloquent\Factories\HasFactory;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Database\Eloquent\Relations\MorphTo;

#[Fillable([
    'reporter_user_id',
    'community_id',
    'reportable_type',
    'reportable_id',
    'reason',
    'details',
    'status',
    'reviewed_by_user_id',
    'reviewed_at',
])]
class Report extends Model
{
    use HasFactory;

    protected function casts(): array
    {
        return [
            'reviewed_at' => 'datetime',
        ];
    }

    public function reporter(): BelongsTo
    {
        return $this->belongsTo(User::class, 'reporter_user_id');
    }

    public function reviewedBy(): BelongsTo
    {
        return $this->belongsTo(User::class, 'reviewed_by_user_id');
    }

    public function community(): BelongsTo
    {
        return $this->belongsTo(Community::class);
    }

    public function reportable(): MorphTo
    {
        return $this->morphTo();
    }
}
