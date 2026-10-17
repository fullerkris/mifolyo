<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use Illuminate\Contracts\Encryption\DecryptException;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Validator;
use Illuminate\Support\Str;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class PasswordRecoveryController extends Controller
{
    public function requestForm(Request $request)
    {
        $returnTo = ForumApi::returnTo($request->query('return_to'));

        return view('password-request', compact('returnTo'));
    }

    public function send(Request $request, ForumApi $forum)
    {
        $input = $request->validate(['email' => ['required', 'string', 'email', 'max:255']]);
        $returnTo = ForumApi::returnTo($request->input('return_to'));
        $back = redirect()->route('account.password.request', ['return_to' => $returnTo])->withInput($input);
        try {
            $response = $forum->call($request, 'post', '/api/auth/forgot-password', $input + ['return_to' => $returnTo]);
        } catch (HttpExceptionInterface) {
            return $back->withErrors(['account' => 'Password reset is temporarily unavailable. Please wait a minute and try again.']);
        }
        if ($response->status() !== 202) {
            return $back->withErrors(['account' => $response->status() === 429
                ? 'Too many attempts. Please wait a minute and try again.'
                : 'Password reset is temporarily unavailable. Please wait a minute and try again.']);
        }

        return $back->with('notice', 'If an account matches that email, a password reset link will be sent. Check your inbox and spam folder.');
    }

    public function form(Request $request)
    {
        if ($request->query->has('token')) {
            $input = $request->only(['token', 'email']);
            $returnTo = ForumApi::returnTo($request->query('return_to'));
            $validator = Validator::make($input, [
                'token' => ['required', 'string', 'size:64', 'regex:/\A[a-f0-9]+\z/'],
                'email' => ['required', 'string', 'email', 'max:255'],
            ]);
            if ($validator->fails()) {
                return response()->view('password-reset', ['context' => null, 'returnTo' => $returnTo], 422);
            }
            $request->session()->put('password_reset', [
                'id' => Str::random(40),
                'token' => Crypt::encryptString($input['token']),
                'email' => $input['email'],
                'return_to' => $returnTo,
            ]);

            // Keep the reset secret out of rendered forms, subsequent URLs and referrers.
            return redirect()->route('account.password.reset');
        }
        $context = $request->session()->get('password_reset');
        $returnTo = ForumApi::returnTo($context['return_to'] ?? null);

        return view('password-reset', compact('context', 'returnTo'));
    }

    public function reset(Request $request, ForumApi $forum)
    {
        $input = $request->validate([
            'password' => ['required', 'string', 'min:8', 'max:1024', 'confirmed'],
            'reset_id' => ['required', 'string', 'size:40'],
        ]);
        $context = $request->session()->get('password_reset');
        $back = redirect()->route('account.password.reset');
        if (! is_array($context) || ! hash_equals($context['id'], $input['reset_id'])) {
            return $back->withErrors(['token' => 'This reset form has expired. Open your reset link again.']);
        }
        try {
            $token = Crypt::decryptString($context['token']);
            $response = $forum->call($request, 'post', '/api/auth/reset-password', [
                'token' => $token, 'email' => $context['email'], 'password' => $input['password'],
                'password_confirmation' => $request->input('password_confirmation'),
            ]);
        } catch (DecryptException) {
            $request->session()->forget('password_reset');

            return $back->withErrors(['token' => 'This reset form has expired. Request a new link.']);
        } catch (HttpExceptionInterface) {
            return $back->withErrors(['account' => 'Could not confirm the password reset. Try signing in with the new password before requesting another link.']);
        }
        if ($response->status() !== 200 || $response->json('reset') !== true) {
            return $back->withErrors($response->status() === 422
                ? ['token' => 'This reset link is invalid or has expired. Request a new link.']
                : ['account' => $response->status() === 429 ? 'Too many attempts. Please wait a minute and try again.' : 'Password reset is temporarily unavailable. Please try again.']);
        }
        $returnTo = ForumApi::returnTo($context['return_to']);
        $request->session()->forget(['forum', 'password_reset']);
        $request->session()->regenerate();
        $request->session()->regenerateToken();

        return redirect()->route('account.sign-in', ['return_to' => $returnTo])
            ->with('notice', 'Your password has been reset. Sign in with your new password to continue.');
    }
}
