<?php

namespace App\Http\Controllers;

use App\Models\User;
use App\Notifications\SearchUiResetPassword;
use App\Services\AccountLifecycle;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Password;
use Illuminate\Validation\Rules;
use Symfony\Component\Mailer\Exception\TransportExceptionInterface;

class AccountController extends Controller
{
    public function forgotPassword(Request $request): JsonResponse
    {
        $input = $request->validate([
            'email' => ['required', 'string', 'email', 'max:255'],
            'return_to' => ['sometimes', 'string', 'max:2048'],
        ]);
        // Mail links use operator configuration, never the request Host or a supplied origin.
        $baseUrl = rtrim((string) config('services.search_ui.url'), '/');
        $parts = parse_url($baseUrl);
        abort_unless(filter_var($baseUrl, FILTER_VALIDATE_URL) && is_array($parts)
            && in_array($parts['scheme'] ?? '', ['http', 'https'], true)
            && ! array_intersect(['user', 'pass', 'query', 'fragment', 'path'], array_keys($parts)), 503, 'Password reset is temporarily unavailable.');
        $returnTo = $input['return_to'] ?? '/';
        if (! str_starts_with($returnTo, '/') || str_starts_with($returnTo, '//') || preg_match('/[\\\\\x00-\x20\x7f]/', $returnTo)) {
            $returnTo = '/';
        }
        try {
            DB::transaction(function () use ($input, $baseUrl, $returnTo) {
                // A failed delivery rolls back token creation so an immediate retry can send.
                User::query()->where('email', $input['email'])->lockForUpdate()->first();
                Password::sendResetLink(['email' => $input['email']], function ($user, $token) use ($baseUrl, $returnTo) {
                    $user->notify(new SearchUiResetPassword($token, $baseUrl, $returnTo));
                });
            });
        } catch (TransportExceptionInterface) {
            abort(503, 'Password reset is temporarily unavailable.');
        }

        // Existing, unknown and broker-throttled emails receive the same response.
        return response()->json(['message' => 'If an account matches that email, a password reset link will be sent.'], 202)
            ->header('Cache-Control', 'private, no-store');
    }

    public function resetPassword(Request $request, AccountLifecycle $accounts): JsonResponse
    {
        $input = $request->validate([
            'email' => ['required', 'string', 'email', 'max:255'],
            'token' => ['required', 'string', 'size:64', 'regex:/\A[a-f0-9]+\z/'],
            'password' => ['required', 'string', 'confirmed', 'max:1024', Rules\Password::defaults()],
        ]);
        $input['password_confirmation'] = $request->input('password_confirmation');
        $status = $accounts->resetPassword($input);

        return ($status === Password::PASSWORD_RESET
            ? response()->json(['reset' => true, 'message' => 'Password reset. Sign in with your new password.'])
            : response()->json(['errors' => ['token' => ['This reset link is invalid or has expired. Request a new link.']]], 422))
            ->header('Cache-Control', 'private, no-store');
    }

    public function destroy(Request $request, AccountLifecycle $accounts): JsonResponse
    {
        $input = $request->validate([
            'password' => ['required', 'string', 'max:1024'],
            'confirmation' => ['required', 'accepted'],
        ]);
        $accounts->delete($request->user('api'), $input['password'], $request->bearerToken() ?? '');

        return response()->json(['deleted' => true])->header('Cache-Control', 'private, no-store');
    }
}
