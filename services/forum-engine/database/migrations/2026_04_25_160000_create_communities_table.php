<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create('communities', function (Blueprint $table) {
            $table->id();
            $table->foreignId('owner_user_id')->nullable()->constrained('users')->nullOnDelete();
            $table->string('name', 120)->unique();
            $table->string('slug', 140)->unique();
            $table->text('description')->nullable();
            $table->boolean('is_private')->default(false);
            $table->boolean('is_nsfw')->default(false);
            $table->unsignedInteger('member_count')->default(0);
            $table->unsignedBigInteger('post_count')->default(0);
            $table->timestamp('last_posted_at')->nullable();
            $table->timestamps();

            $table->index(['is_private', 'created_at']);
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('communities');
    }
};
