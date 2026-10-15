<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Crypt;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class ForumAccountController extends Controller
{
    public function form(Request $request)
    {
        $register = $request->routeIs('account.register');
        $returnTo = ForumApi::returnTo($request->query('return_to'));
        if ($request->session()->has('forum.user')) {
            return redirect($returnTo);
        }

        return view('account', compact('register', 'returnTo'));
    }

    public function authenticate(Request $request, ForumApi $forum)
    {
        $register = $request->routeIs('account.register.submit');
        $rules = ['email' => ['required', 'string', 'email', 'max:255'], 'password' => ['required', 'string', 'max:1024']];
        if ($register) {
            $rules += ['name' => ['required', 'string', 'max:255'], 'username' => ['required', 'string', 'min:3', 'max:40', 'regex:/^[a-z0-9_-]+$/']];
            $rules['password'][] = 'min:8';
            $rules['password'][] = 'confirmed';
        }
        $input = $request->validate($rules);
        if ($register) {
            $input['password_confirmation'] = $request->input('password_confirmation');
        }
        $returnTo = ForumApi::returnTo($request->input('return_to'));
        $back = redirect()->route($register ? 'account.register' : 'account.sign-in', ['return_to' => $returnTo])
            ->withInput($request->only(['name', 'username', 'email']));
        try {
            $response = $forum->call($request, 'post', $register ? '/api/auth/register' : '/api/auth/login', $input);
        } catch (HttpExceptionInterface) {
            return $back->withErrors(['account' => $register ? 'Could not confirm account creation. Try signing in before registering again.' : 'Sign-in is temporarily unavailable. Please try again.']);
        }
        if (! $response->successful()) {
            return $back->withErrors($response->status() === 422
                ? ($response->json('errors') ?: ['account' => $response->json('message') ?: 'Check your details and try again.'])
                : ['account' => $response->status() === 429 ? 'Too many attempts. Please wait a minute and try again.' : 'The account service is temporarily unavailable. Please try again.']);
        }
        $user = $response->json('data.user');
        $token = $response->json('data.token');
        if (! is_array($user) || ! isset($user['id'], $user['username']) || ! is_string($token) || $token === '') {
            return $back->withErrors(['account' => 'The account service returned an unexpected response. Please try signing in again.']);
        }
        $request->session()->regenerate();
        $request->session()->put('forum', [
            'token' => Crypt::encryptString($token),
            'user' => array_intersect_key($user, array_flip(['id', 'username', 'name'])),
        ]);

        return redirect($returnTo)->with('notice', $register ? 'Your account is ready.' : 'Welcome back.');
    }

    public function logout(Request $request, ForumApi $forum)
    {
        try {
            if ($request->session()->has('forum.token')) {
                $forum->call($request, 'post', '/api/auth/logout');
            }
        } catch (HttpExceptionInterface) {
            // Always clear this browser's session, even if revocation is unavailable.
        } finally {
            $request->session()->invalidate();
            $request->session()->regenerateToken();
        }

        return redirect('/')->with('notice', 'You are signed out of this browser.');
    }
}
