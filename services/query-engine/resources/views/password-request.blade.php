<x-search-layout title="Reset your password — MiFolyo" :home="true">
    <main id="main" class="mf-page mf-account-page" tabindex="-1">
        <section class="mf-account-card">
            <p class="mf-eyebrow">Your account</p>
            <h1>Reset your password</h1>
            <p class="mf-account-intro">Enter your account email. We’ll send you a link to choose a new password.</p>
            @if ($errors->any())<div class="mf-form-error" role="alert"><ul>@foreach ($errors->all() as $error)<li>{{ $error }}</li>@endforeach</ul></div>@endif
            <form method="POST" action="{{ route('account.password.email') }}" class="mf-account-form" data-posting-form>
                @csrf
                <input type="hidden" name="return_to" value="{{ $returnTo }}">
                <label for="reset-email">Email</label><input id="reset-email" type="email" name="email" autocomplete="email" value="{{ old('email') }}" maxlength="255" required>
                <button class="mf-button mf-primary" type="submit">Send reset link</button>
            </form>
            <p class="mf-account-switch"><a class="mf-text-link" href="{{ route('account.sign-in', ['return_to' => $returnTo]) }}">Back to sign in</a></p>
        </section>
    </main>
</x-search-layout>
