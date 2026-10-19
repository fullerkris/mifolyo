<?php

namespace App\Support;

use Illuminate\Http\Request;

class ThreadContext
{
    public static function url(Request $request, int $thread, ?int $comment = null): string
    {
        $parameters = ['thread' => $thread] + SearchContext::parameters($request);
        $replyTo = filter_var($request->input('reply_to'), FILTER_VALIDATE_INT, ['options' => ['min_range' => 0]]);
        if ($replyTo !== false && $replyTo !== null) {
            $parameters['reply_to'] = $replyTo;
        }

        return route('threads.show', $parameters, false).($comment ? '#comment-'.$comment : '#thread-votes');
    }
}
