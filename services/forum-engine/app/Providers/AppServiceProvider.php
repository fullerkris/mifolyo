<?php

namespace App\Providers;

use App\Models\Comment;
use App\Models\Post;
use App\Policies\CommentPolicy;
use App\Policies\PostPolicy;
use Illuminate\Cache\RateLimiting\Limit;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Gate;
use Illuminate\Support\Facades\RateLimiter;
use Illuminate\Support\ServiceProvider;

class AppServiceProvider extends ServiceProvider
{
    /**
     * Register any application services.
     */
    public function register(): void
    {
        //
    }

    /**
     * Bootstrap any application services.
     */
    public function boot(): void
    {
        Gate::policy(Post::class, PostPolicy::class);
        Gate::policy(Comment::class, CommentPolicy::class);

        RateLimiter::for('auth', function (Request $request): Limit {
            return Limit::perMinute(20)->by($request->ip());
        });

        RateLimiter::for('api-read', function (Request $request): Limit {
            return Limit::perMinute(180)->by((string) ($request->user('api')?->id ?? $request->ip()));
        });

        RateLimiter::for('api-write', function (Request $request): Limit {
            return Limit::perMinute(90)->by((string) ($request->user('api')?->id ?? $request->ip()));
        });

        RateLimiter::for('mod-actions', function (Request $request): Limit {
            return Limit::perMinute(60)->by((string) ($request->user('api')?->id ?? $request->ip()));
        });
    }
}
