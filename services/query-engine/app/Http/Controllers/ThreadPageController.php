<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use App\Support\SearchContext;
use Illuminate\Http\Request;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class ThreadPageController extends Controller
{
    public function show(Request $request, int $thread, ForumApi $forum)
    {
        $post = $forum->data($forum->call($request, 'get', '/api/posts/'.$thread));
        abort_unless(isset($post['id'], $post['title']), 503);
        $commentsUnavailable = false;
        try {
            $comments = $forum->data($forum->call($request, 'get', '/api/posts/'.$thread.'/comments'));
        } catch (HttpExceptionInterface $exception) {
            if (! in_array($exception->getStatusCode(), [429, 503], true)) {
                throw $exception;
            }
            $comments = [];
            $commentsUnavailable = true;
        }
        $draft = $request->session()->get('forum_drafts.'.$thread, []);
        $replyTo = filter_var($request->query('reply_to', $draft['parent_comment_id'] ?? null), FILTER_VALIDATE_INT) ?: null;
        $reply = null;
        $find = function (array $nodes) use (&$find, &$reply, $replyTo): void {
            foreach ($nodes as $node) {
                if (($node['id'] ?? null) === $replyTo && ! ($node['is_removed'] ?? false) && ! ($node['is_locked'] ?? false) && ($node['depth'] ?? 0) < 8) {
                    $reply = $node;
                }
                $find($node['children'] ?? []);
            }
        };
        $find($comments);
        $replyUnavailable = $replyTo !== null && $reply === null;
        $sourceUrl = $post['source_url'] ?? $post['url'] ?? '';
        if (! is_string($sourceUrl) || ! filter_var($sourceUrl, FILTER_VALIDATE_URL) || ! in_array(strtolower(parse_url($sourceUrl, PHP_URL_SCHEME) ?? ''), ['http', 'https'], true)) {
            $sourceUrl = '';
        }

        return response()->view('thread', compact('post', 'comments', 'commentsUnavailable', 'reply', 'replyUnavailable', 'draft', 'sourceUrl'))
            ->header('Cache-Control', 'private, no-store');
    }

    public function comment(Request $request, int $thread, ForumApi $forum)
    {
        $input = $request->validate([
            'body' => ['required', 'string', 'max:10000'],
            'parent_comment_id' => ['nullable', 'integer', 'min:1'],
        ]);
        $request->session()->put('forum_drafts.'.$thread, $input);
        $back = route('threads.show', ['thread' => $thread] + SearchContext::parameters($request), false);
        if (! $request->session()->has('forum.user')) {
            return redirect()->route('account.sign-in', ['return_to' => $back]);
        }
        try {
            $response = $forum->call($request, 'post', '/api/posts/'.$thread.'/comments', $input);
        } catch (HttpExceptionInterface) {
            return redirect($back)->withErrors(['body' => 'Could not confirm whether your reply was posted. Check the conversation before trying again.']);
        }
        if ($response->status() === 401) {
            return redirect()->route('account.sign-in', ['return_to' => $back])->with('notice', 'Please sign in again to post your reply. Your draft is saved.');
        }
        if (! $response->successful()) {
            $message = in_array($response->status(), [403, 422, 423, 429], true)
                ? ($response->json('message') ?? 'Your reply could not be posted.') : 'Could not confirm whether your reply was posted. Check the conversation before trying again.';

            return redirect($back)->withErrors(['body' => $message]);
        }
        if (! is_int($response->json('data.id')) || $response->json('data.id') < 1) {
            return redirect($back)->withErrors(['body' => 'Could not confirm whether your reply was posted. Check the conversation before trying again.']);
        }
        $request->session()->forget('forum_drafts.'.$thread);

        return redirect()->to($back.'#comment-'.(int) $response->json('data.id'))->with('notice', 'Your reply was posted.');
    }
}
