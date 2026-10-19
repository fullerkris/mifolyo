<x-search-layout title="Choose a new password — MiFolyo" :home="true">
    <main id="main" class="mf-page mf-account-page" tabindex="-1">
        <section class="mf-account-card">
            <p class="mf-eyebrow">Your account</p>
            <h1>Choose a new password</h1>
            @if ($errors->any())<div class="mf-form-error" role="alert"><ul>@foreach ($errors->all() as $error)<li>{{ $error }}</li>@endforeach</ul></div>@endif
            @if ($context && ! $errors->has('token'))
                <p class="mf-account-intro">Resetting the password for <strong>{{ $context['email'] }}</strong>. Use at least 8 characters. You’ll sign in again after resetting.</p>
                <form method="POST" action="{{ route('account.password.update') }}" class="mf-account-form" data-posting-form>
                    @csrf
                    <input type="hidden" name="reset_id" value="{{ $context['id'] }}">
                    <label for="new-password">New password</label><input id="new-password" type="password" name="password" autocomplete="new-password" minlength="8" maxlength="1024" required>
                    <label for="confirm-password">Confirm new password</label><input id="confirm-password" type="password" name="password_confirmation" autocomplete="new-password" minlength="8" maxlength="1024" required>
                    <button class="mf-button mf-primary" type="submit">Reset password</button>
                </form>
            @else
                <p class="mf-account-intro">Open the reset link from your email, or request a new link below.</p>
            @endif
            <p class="mf-account-switch"><a class="mf-text-link" href="{{ route('account.password.request', ['return_to' => $returnTo]) }}">Request a new reset link</a></p>
            <p class="mf-account-switch"><a class="mf-text-link" href="{{ route('account.sign-in', ['return_to' => $returnTo]) }}">Back to sign in</a></p>
        </section>
    </main>
</x-search-layout>
