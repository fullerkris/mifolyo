<?php

namespace App\Jobs;

use App\Models\ModerationAction;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Bus\Dispatchable;
use Illuminate\Foundation\Queue\Queueable;
use Illuminate\Queue\InteractsWithQueue;
use Illuminate\Queue\SerializesModels;
use Illuminate\Support\Facades\Log;

class ProcessModerationActionJob implements ShouldQueue
{
    use Dispatchable;
    use InteractsWithQueue;
    use Queueable;
    use SerializesModels;

    public function __construct(public readonly int $moderationActionId)
    {
        $this->onQueue('moderation');
    }

    public function handle(): void
    {
        $action = ModerationAction::query()->find($this->moderationActionId);

        if (! $action) {
            return;
        }

        Log::info('forum.moderation.action.created', [
            'moderation_action_id' => (int) $action->id,
            'community_id' => (int) $action->community_id,
            'actor_user_id' => (int) $action->actor_user_id,
            'action' => (string) $action->action,
            'target_type' => (string) $action->actionable_type,
            'target_id' => (int) $action->actionable_id,
        ]);
    }
}
