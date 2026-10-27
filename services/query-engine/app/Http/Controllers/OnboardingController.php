<?php

namespace App\Http\Controllers;

use App\Services\ForumApi;
use App\Services\Onboarding;
use Illuminate\Http\Request;

class OnboardingController extends Controller
{
    public function show(Request $request)
    {
        abort_unless($request->session()->has(['forum.user', 'forum.token']), 401);
        abort_unless($request->attributes->has('onboarding'), 503);

        return response()->json(['user_id' => $request->session()->get('forum.user.id'), 'data' => Onboarding::state($request)])
            ->header('Cache-Control', 'private, no-store');
    }

    public function update(Request $request, Onboarding $onboarding)
    {
        abort_unless($request->session()->has(['forum.user', 'forum.token']), 401);
        $input = $request->validate([
            'step' => ['required', 'string', 'in:welcome,discuss'],
            'user_id' => ['required', 'integer', 'min:1'],
        ]);
        abort_unless((string) $input['user_id'] === (string) $request->session()->get('forum.user.id'), 409);
        $saved = $onboarding->complete($request, $input['step']);
        if ($request->expectsJson()) {
            return $saved ? $this->show($request)
                : response()->json(['message' => 'We couldn’t save your onboarding progress. Please try again.'], 503);
        }
        $back = redirect(ForumApi::returnTo($request->input('return_to')));

        return $saved ? $back : $back->withErrors(['onboarding' => 'We couldn’t save your onboarding progress. Please try again.']);
    }
}
