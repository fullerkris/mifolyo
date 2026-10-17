<x-search-layout title="Your account — MiFolyo" :home="true">
    <main id="main" class="mf-page mf-account-page" tabindex="-1">
        <a class="mf-text-link mf-back-link" href="{{ $returnTo }}">← Back to browsing</a>
        <section class="mf-account-card">
            <p class="mf-eyebrow">Your account</p>
            <h1>Account settings</h1>
            <p class="mf-account-intro"><x-forum-author :author="$user" /></p>
            <p class="mf-account-email">{{ $user['email'] }}</p>
            <p class="mf-account-switch"><a class="mf-text-link" href="{{ route('account.password.request', ['return_to' => route('account.show', ['return_to' => $returnTo], false)]) }}">Reset your password</a></p>
            @if ($errors->any())<div class="mf-form-error" role="alert"><ul>@foreach ($errors->all() as $error)<li>{{ $error }}</li>@endforeach</ul></div>@endif
            <section class="mf-account-delete" aria-labelledby="delete-heading">
                <h2 id="delete-heading">Delete your account</h2>
                <p id="delete-description" class="mf-account-intro">This permanently deletes your account and removes your votes. Your posts and comments remain under “Community member”. This cannot be undone.</p>
                <form method="POST" action="{{ route('account.destroy') }}" class="mf-account-form" data-posting-form aria-describedby="delete-description">
                    @csrf
                    @method('DELETE')
                    <input type="hidden" name="return_to" value="{{ $returnTo }}">
                    <label for="delete-password">Current password</label><input id="delete-password" type="password" name="password" autocomplete="current-password" maxlength="1024" required>
                    <label class="mf-confirm-delete" for="delete-confirm"><input id="delete-confirm" type="checkbox" name="confirmation" value="1" required> I understand and want to delete my account.</label>
                    <button class="mf-button mf-danger" type="submit">Delete my account</button>
                </form>
            </section>
        </section>
    </main>
</x-search-layout>
