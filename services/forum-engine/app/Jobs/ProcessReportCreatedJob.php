<?php

namespace App\Jobs;

use App\Models\Report;
use Illuminate\Contracts\Queue\ShouldQueue;
use Illuminate\Foundation\Bus\Dispatchable;
use Illuminate\Foundation\Queue\Queueable;
use Illuminate\Queue\InteractsWithQueue;
use Illuminate\Queue\SerializesModels;
use Illuminate\Support\Facades\Log;

class ProcessReportCreatedJob implements ShouldQueue
{
    use Dispatchable;
    use InteractsWithQueue;
    use Queueable;
    use SerializesModels;

    public function __construct(public readonly int $reportId)
    {
        $this->onQueue('moderation');
    }

    public function handle(): void
    {
        $report = Report::query()->find($this->reportId);

        if (! $report) {
            return;
        }

        Log::info('forum.report.created', [
            'report_id' => (int) $report->id,
            'community_id' => (int) $report->community_id,
            'reportable_type' => (string) $report->reportable_type,
            'reportable_id' => (int) $report->reportable_id,
            'status' => (string) $report->status,
        ]);
    }
}
