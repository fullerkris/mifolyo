<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use App\Support\SearchContext;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class BrowserForumController extends Controller
{
    public function counts(Request $request, ForumApi $forum)
    {
        $input = $request->validate([
            'urls' => ['required', 'array', 'list', 'min:1', 'max:20'],
            'urls.*' => ['required', 'string', 'max:2048'],
        ]);
        $data = $forum->data($forum->call($request, 'post', '/api/threads/counts', $input));
        abort_unless(array_is_list($data) && count($data) === count($input['urls']), 503);
        foreach ($data as $index => $item) {
            abort_unless(is_array($item) && ($item['url'] ?? null) === $input['urls'][$index]
                && array_key_exists('count', $item) && ($item['count'] === null || (is_int($item['count']) && $item['count'] >= 0)), 503);
        }

        return response()->json(['data' => $data])->header('Cache-Control', 'private, no-store');
    }

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
        $context = SearchContext::parameters($request);
        $back = route('discussions', ['url' => $input['source_url']] + $context, false);
        if (! $request->session()->has('forum.user')) {
            return $request->expectsJson() ? response()->json(['message' => 'Sign in to start a thread.'], 401)
                : redirect()->route('account.sign-in', ['return_to' => $back]);
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
                : redirect()->route('threads.show', ['thread' => $response->json('data.id')] + $context);
        }
        if ($response->status() === 401 && ! $request->expectsJson()) {
            return redirect()->route('account.sign-in', ['return_to' => $back])
                ->with('notice', 'Please sign in again to create your thread. Your draft is saved.');
        }
        $status = in_array($response->status(), [401, 403, 422, 429], true) ? $response->status() : 503;
        $message = $status === 503 ? 'Could not confirm whether your thread was created. Refresh the discussions before trying again.'
            : ($response->json('message') ?: 'Could not create the thread.');

        return $request->expectsJson() ? response()->json(['message' => $message], $status) : redirect($back)->withErrors(['thread' => $message]);
    }
}
