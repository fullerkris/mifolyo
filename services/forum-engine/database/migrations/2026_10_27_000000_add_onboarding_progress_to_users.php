<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    public function up(): void
    {
        Schema::table('users', function (Blueprint $table) {
            // Existing accounts are deliberately not enrolled by the migration.
            $table->timestampTz('onboarding_started_at')->nullable();
            $table->timestampTz('onboarding_welcome_at')->nullable();
            $table->timestampTz('onboarding_discuss_at')->nullable();
            $table->timestampTz('onboarding_vote_at')->nullable();
        });
    }

    public function down(): void
    {
        Schema::table('users', function (Blueprint $table) {
            $table->dropColumn(['onboarding_started_at', 'onboarding_welcome_at', 'onboarding_discuss_at', 'onboarding_vote_at']);
        });
    }
};
