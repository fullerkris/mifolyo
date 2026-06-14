<?php

use App\Http\Controllers\AuthController;
use App\Http\Controllers\CommentController;
use App\Http\Controllers\CommunityController;
use App\Http\Controllers\FeedController;
use App\Http\Controllers\HealthController;
use App\Http\Controllers\ModerationController;
use App\Http\Controllers\PostController;
use App\Http\Controllers\ReportController;
use App\Http\Controllers\ThreadController;
use App\Http\Controllers\VoteController;
use Illuminate\Support\Facades\Route;

Route::middleware('throttle:api-read')->group(function () {
    Route::get('/health/live', [HealthController::class, 'live']);
    Route::get('/health/ready', [HealthController::class, 'ready']);
    Route::get('/feeds/home', [FeedController::class, 'home']);
    Route::get('/feeds/community/{community:slug}', [FeedController::class, 'community']);
    Route::get('/communities', [CommunityController::class, 'index']);
    Route::get('/threads/by-url', [ThreadController::class, 'byUrl']);
    Route::get('/posts/{post}/comments', [CommentController::class, 'index']);
    Route::get('/posts/{post}', [PostController::class, 'show']);
});

Route::middleware('throttle:auth')->group(function () {
    Route::post('/auth/register', [AuthController::class, 'register']);
    Route::post('/auth/login', [AuthController::class, 'login']);
});

Route::middleware(['auth:api', 'auth.token.fresh', 'throttle:api-read'])->group(function () {
    Route::get('/auth/me', [AuthController::class, 'me']);
    Route::post('/auth/refresh', [AuthController::class, 'refresh']);
    Route::get('/mod/queue', [ModerationController::class, 'queue']);
    Route::get('/mod/actions', [ModerationController::class, 'actions']);
});

Route::middleware(['auth:api', 'auth.token.fresh', 'throttle:api-write'])->group(function () {
    Route::post('/auth/logout', [AuthController::class, 'logout']);
    Route::post('/communities', [CommunityController::class, 'store']);
    Route::post('/communities/{community:slug}/join', [CommunityController::class, 'join']);
    Route::post('/threads', [ThreadController::class, 'store']);
    Route::post('/posts', [PostController::class, 'store']);
    Route::post('/posts/{post}/comments', [CommentController::class, 'store']);
    Route::post('/votes', [VoteController::class, 'store']);
    Route::post('/reports', [ReportController::class, 'store']);
});

Route::middleware(['auth:api', 'auth.token.fresh', 'throttle:mod-actions'])->group(function () {
    Route::post('/mod/actions', [ModerationController::class, 'action']);
});
