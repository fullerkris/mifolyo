<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class AccountSettingsController extends Controller
{
    public function show(Request $request, ForumApi $forum)
    {
        $returnTo = ForumApi::returnTo($request->query('return_to'));
        if (! $request->session()->has('forum.token')) {
            return $this->signIn($returnTo);
        }
        $response = $forum->call($request, 'get', '/api/auth/me');
        if ($response->status() === 401) {
            return $this->signIn($returnTo);
        }
        $user = $forum->data($response);
        abort_unless(isset($user['id']) && is_string($user['username'] ?? null) && is_string($user['email'] ?? null), 503);

        return view('account-settings', compact('user', 'returnTo'));
    }

    public function destroy(Request $request, ForumApi $forum)
    {
        $returnTo = ForumApi::returnTo($request->input('return_to'));
        if (! $request->session()->has('forum.token')) {
            return $this->signIn($returnTo);
        }
        $input = $request->validate([
            'password' => ['required', 'string', 'max:1024'],
            'confirmation' => ['required', 'accepted'],
        ]);
        $back = redirect()->route('account.show', ['return_to' => $returnTo]);
        try {
            $response = $forum->call($request, 'delete', '/api/auth/account', $input);
        } catch (HttpExceptionInterface) {
            return $back->withErrors(['account' => 'Could not confirm account deletion. Try signing in to check your account before trying again.']);
        }
        if ($response->status() === 401) {
            return $this->signIn($returnTo)->with('notice', 'Please sign in again before managing your account.');
        }
        if ($response->status() !== 200 || $response->json('deleted') !== true) {
            return $back->withErrors(['account' => match ($response->status()) {
                422 => 'Check your current password and confirm that you want to delete your account.',
                429 => 'Too many attempts. Please wait a minute and try again.',
                default => 'Could not confirm account deletion. Try signing in to check your account before trying again.',
            }]);
        }
        $request->session()->invalidate();
        $request->session()->regenerateToken();

        return redirect('/')->with('notice', 'Your account has been deleted. Your discussions remain under Community member.');
    }

    private function signIn(string $returnTo)
    {
        return redirect()->route('account.sign-in', [
            'return_to' => route('account.show', ['return_to' => $returnTo], false),
        ]);
    }
}
