@if (\App\Services\Onboarding::state(request())['welcome'] && ! \App\Services\Onboarding::isSearch(request()) && request()->routeIs('home', 'search', 'search.exact', 'search.images', 'discussions', 'threads.show', 'page.links'))
    <section class="mf-onboarding-welcome" data-onboarding-step="welcome" aria-labelledby="onboarding-welcome-title">
        <div>
            <p class="mf-eyebrow">A quick hello</p>
            <h2 id="onboarding-welcome-title">Welcome to MiFolyo.</h2>
            <p>Search for a useful source, then choose <strong>Discuss</strong> to see what other readers think. You can join a conversation or start the first one.</p>
        </div>
        <form method="POST" action="{{ route('onboarding.update') }}" data-onboarding-dismiss>
            @csrf
            <input type="hidden" name="step" value="welcome">
            <input type="hidden" name="user_id" value="{{ session('forum.user.id') }}">
            <input type="hidden" name="return_to" value="{{ \App\Services\ForumApi::returnTo(request()->getRequestUri()) }}">
            <button class="mf-button mf-secondary" type="submit">Got it <span aria-hidden="true">→</span></button>
            <p class="mf-form-error" data-onboarding-status role="status">{{ $errors->first('onboarding') }}</p>
        </form>
    </section>
@endif
