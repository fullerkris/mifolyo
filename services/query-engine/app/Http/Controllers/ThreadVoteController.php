<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use App\Support\ThreadContext;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Validator;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

class ThreadVoteController extends Controller
{
    public function store(Request $request, int $thread, ForumApi $forum)
    {
        abort_if($thread < 1, 404);
        $validator = Validator::make($request->all(), [
            'votable_type' => ['required', 'string', 'in:post,comment'],
            'votable_id' => ['required', 'integer', 'min:1'],
            'value' => ['required', 'integer', 'in:-1,0,1'],
        ]);
        if ($validator->fails()) {
            return $this->failure($request, $thread, 422);
        }
        $input = $validator->validated();
        $input['votable_id'] = (int) $input['votable_id'];
        $input['value'] = (int) $input['value'];
        if ($input['votable_type'] === 'post' && $input['votable_id'] !== $thread) {
            return $this->failure($request, $thread, 422);
        }
        if (! $request->session()->has(['forum.user', 'forum.token'])) {
            return $this->failure($request, $thread, 401);
        }
        try {
            $response = $forum->call($request, 'post', '/api/votes', $input + ['post_id' => $thread]);
        } catch (HttpExceptionInterface) {
            return $this->failure($request, $thread, 503);
        }
        if (! $response->successful()) {
            $status = in_array($response->status(), [401, 403, 404, 422, 423, 429], true) ? $response->status() : 503;
            $retryAfter = filter_var($response->header('Retry-After'), FILTER_VALIDATE_INT, ['options' => ['min_range' => 1, 'max_range' => 3600]]);

            return $this->failure($request, $thread, $status, $status === 429 ? ($retryAfter ?: 60) : 0);
        }
        $data = $response->json('data.votable');
        if (! is_array($data) || ($data['type'] ?? null) !== $input['votable_type'] || ($data['id'] ?? null) !== $input['votable_id']
            || ! is_int($data['score'] ?? null) || $response->json('data.viewer_vote') !== $input['value']) {
            return $this->failure($request, $thread, 503);
        }
        $message = match ($input['value']) {
            1 => 'Upvote saved.', -1 => 'Downvote saved.', default => 'Vote removed.',
        };

        return $request->expectsJson()
            ? response()->json(['data' => ['type' => $data['type'], 'id' => $data['id'], 'score' => $data['score'], 'viewer_vote' => $input['value']], 'message' => $message])->header('Cache-Control', 'private, no-store')
            : redirect(ThreadContext::url($request, $thread, $input['votable_type'] === 'comment' ? $input['votable_id'] : null))
                ->with('vote_feedback', ['type' => $input['votable_type'], 'id' => $input['votable_id'], 'message' => $message]);
    }

    private function failure(Request $request, int $thread, int $status, int $retryAfter = 0)
    {
        $id = filter_var($request->input('votable_id'), FILTER_VALIDATE_INT, ['options' => ['min_range' => 1]]) ?: $thread;
        $type = $request->input('votable_type') === 'comment' ? 'comment' : 'post';
        $back = ThreadContext::url($request, $thread, $type === 'comment' ? $id : null);
        $signIn = route('account.sign-in', ['return_to' => $back], false);
        $message = match ($status) {
            401 => 'Please sign in to vote. You’ll return to this conversation.',
            403 => 'You no longer have access to vote in this conversation.',
            404 => 'This thread or comment is no longer available.',
            422 => 'This vote could not be saved. Reload the conversation and try again.',
            423 => 'Voting is closed for this thread or comment.',
            429 => 'Too many votes. Please wait '.$retryAfter.' seconds before trying again.',
            default => 'We couldn’t confirm your vote. Retry to save the same choice.',
        };
        $headers = $retryAfter ? ['Retry-After' => (string) $retryAfter] : [];
        if ($request->expectsJson()) {
            return response()->json(['message' => $message, 'sign_in_url' => $signIn, 'reload_url' => $back], $status, $headers)
                ->header('Cache-Control', 'private, no-store');
        }
        if ($status === 401) {
            return redirect($signIn)->with('notice', $message);
        }
        $value = filter_var($request->input('value'), FILTER_VALIDATE_INT);
        $feedback = ['type' => $type, 'id' => $id, 'message' => $message, 'error' => true];
        if (in_array($status, [429, 503], true) && in_array($value, [-1, 0, 1], true)) {
            $feedback['retry_value'] = $value;
        }

        return redirect($back)->with('vote_feedback', $feedback)->withHeaders($headers);
    }
}
