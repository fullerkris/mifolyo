<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class BrowserForumController extends Controller
{
    public function index(Request $request, ForumApi $forum)
    {
        $input = $request->validate([
            'url' => ['required', 'url:http,https', 'max:2048'],
            'sort' => ['sometimes', 'in:top,new'],
            'page' => ['sometimes', 'integer', 'min:1', 'max:10000'],
            'per_page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ]);
        $response = $forum->call($request, 'get', '/api/threads/by-url', $input);
        abort_unless(is_array($response->json()), 503);

        return response()->json($response->json(), $response->status());
    }

    public function create(Request $request, ForumApi $forum)
    {
        $input = $request->validate([
            'title' => ['required', 'string', 'max:300'],
            'body' => ['nullable', 'string', 'max:50000'],
            'source_url' => ['required', 'url:http,https', 'max:2048'],
        ]);
        $request->session()->put('forum_thread_draft', $input);
        $back = route('discussions', ['url' => $input['source_url']]);
        if (! $request->session()->has('forum.user')) {
            return $request->expectsJson() ? response()->json(['message' => 'Sign in to start a thread.'], 401)
                : redirect()->route('account.sign-in', ['return_to' => '/discussions?'.http_build_query(['url' => $input['source_url']])]);
        }
        try {
            $response = $forum->call($request, 'post', '/api/threads', $input + ['community_slug' => config('services.forum.default_community_slug')]);
        } catch (HttpExceptionInterface) {
            $message = 'Could not confirm whether your thread was created. Refresh the discussions before trying again.';

            return $request->expectsJson() ? response()->json(['message' => $message], 503) : redirect($back)->withErrors(['thread' => $message]);
        }
        if ($response->successful() && is_int($response->json('data.id')) && $response->json('data.id') > 0) {
            $request->session()->forget('forum_thread_draft');

            return $request->expectsJson() ? response()->json($response->json(), $response->status())
                : redirect()->route('threads.show', $response->json('data.id'));
        }
        if ($response->status() === 401 && ! $request->expectsJson()) {
            return redirect()->route('account.sign-in', ['return_to' => '/discussions?'.http_build_query(['url' => $input['source_url']])])
                ->with('notice', 'Please sign in again to create your thread. Your draft is saved.');
        }
        $status = in_array($response->status(), [401, 403, 422, 429], true) ? $response->status() : 503;
        $message = $status === 503 ? 'Could not confirm whether your thread was created. Refresh the discussions before trying again.'
            : ($response->json('message') ?: 'Could not create the thread.');

        return $request->expectsJson() ? response()->json(['message' => $message], $status) : redirect($back)->withErrors(['thread' => $message]);
    }
}
