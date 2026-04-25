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
        Schema::create('posts', function (Blueprint $table) {
            $table->id();
            $table->foreignId('community_id')->constrained()->cascadeOnDelete();
            $table->foreignId('author_user_id')->nullable()->constrained('users')->nullOnDelete();
            $table->string('title', 300);
            $table->string('slug', 320);
            $table->text('body')->nullable();
            $table->string('url')->nullable();
            $table->enum('content_type', ['text', 'link'])->default('text');
            $table->integer('score')->default(0);
            $table->unsignedInteger('comment_count')->default(0);
            $table->unsignedInteger('upvote_count')->default(0);
            $table->unsignedInteger('downvote_count')->default(0);
            $table->boolean('is_locked')->default(false);
            $table->boolean('is_removed')->default(false);
            $table->boolean('is_nsfw')->default(false);
            $table->timestamp('published_at')->nullable();
            $table->timestamps();

            $table->unique(['community_id', 'slug']);
            $table->index(['community_id', 'published_at']);
            $table->index(['author_user_id', 'created_at']);
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('posts');
    }
};
