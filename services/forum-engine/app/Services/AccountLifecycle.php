<?php

namespace App\Services;

use App\Models\Comment;
use App\Models\Community;
use App\Models\Post;
use App\Models\User;
use Illuminate\Auth\Events\PasswordReset;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Facades\Password;
use Illuminate\Support\Str;
use Illuminate\Validation\ValidationException;

class AccountLifecycle
{
    public function resetPassword(#[\SensitiveParameter] array $credentials): string
    {
        return DB::transaction(function () use ($credentials): string {
            // Serialize token consumption, reset and deletion for this account.
            User::query()->where('email', $credentials['email'])->lockForUpdate()->first();

            return Password::reset($credentials, function (User $user, string $password) {
                $user->forceFill([
                    'password' => Hash::make($password),
                    'remember_token' => Str::random(60),
                    'api_token' => null,
                    'api_token_expires_at' => null,
                    'api_token_last_used_at' => null,
                ])->save();
                DB::table('sessions')->where('user_id', $user->id)->delete();
                DB::afterCommit(fn () => event(new PasswordReset($user)));
            });
        }, 3);
    }

    public function delete(User $user, #[\SensitiveParameter] string $password, #[\SensitiveParameter] ?string $apiToken = null, string $errorBag = 'default'): void
    {
        DB::transaction(function () use ($user, $password, $apiToken, $errorBag) {
            $locked = User::query()->lockForUpdate()->find($user->id);
            abort_unless($locked, 401);
            if ($apiToken !== null) {
                abort_unless($apiToken !== '' && $locked->api_token
                    && hash_equals($locked->api_token, hash('sha256', $apiToken))
                    && ! $locked->api_token_expires_at?->isPast(), 401);
            }
            if (! Hash::check($password, $locked->password)) {
                throw ValidationException::withMessages(['password' => 'The password is incorrect.'])->errorBag($errorBag);
            }

            // Votes take this same user -> parent post -> comment lock order.
            // Restore totals from remaining vote truth before the FK cascade.
            $votes = $locked->votes()->get();
            $commentIds = $votes->where('votable_type', Comment::class)->pluck('votable_id');
            $postIds = $votes->where('votable_type', Post::class)->pluck('votable_id')
                ->merge(Comment::query()->whereIn('id', $commentIds)->pluck('post_id'))->unique();
            Post::query()->whereIn('id', $postIds)->orderBy('id')->lockForUpdate()->get();
            $targets = Post::query()->whereIn('id', $votes->where('votable_type', Post::class)->pluck('votable_id'))->get()
                ->concat(Comment::query()->whereIn('id', $commentIds)->orderBy('id')->lockForUpdate()->get());
            foreach ($targets as $target) {
                $remaining = $target->votes()->where('user_id', '!=', $locked->id);
                $up = (clone $remaining)->where('value', 1)->count();
                $down = (clone $remaining)->where('value', -1)->count();
                $target->update(['score' => $up - $down, 'upvote_count' => $up, 'downvote_count' => $down]);
            }
            $communities = Community::query()->whereIn('id', $locked->memberships()->pluck('community_id'))
                ->orderBy('id')->lockForUpdate()->get();
            Password::deleteToken($locked);
            DB::table('sessions')->where('user_id', $locked->id)->delete();
            $locked->delete();
            foreach ($communities as $community) {
                $community->update(['member_count' => $community->memberships()->count()]);
            }
        }, 3);
    }
}
