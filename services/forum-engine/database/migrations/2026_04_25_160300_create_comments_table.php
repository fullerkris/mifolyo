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
        Schema::create('comments', function (Blueprint $table) {
            $table->id();
            $table->foreignId('post_id')->constrained()->cascadeOnDelete();
            $table->foreignId('parent_comment_id')->nullable()->constrained('comments')->cascadeOnDelete();
            $table->foreignId('author_user_id')->nullable()->constrained('users')->nullOnDelete();
            $table->text('body');
            $table->unsignedTinyInteger('depth')->default(0);
            $table->integer('score')->default(0);
            $table->unsignedInteger('upvote_count')->default(0);
            $table->unsignedInteger('downvote_count')->default(0);
            $table->boolean('is_removed')->default(false);
            $table->boolean('is_locked')->default(false);
            $table->timestamps();

            $table->index(['post_id', 'created_at']);
            $table->index(['parent_comment_id']);
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('comments');
    }
};
