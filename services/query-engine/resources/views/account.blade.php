<x-search-layout :title="($register ? 'Create your account' : 'Sign in').' — MiFolyo'" :home="true">
    <main id="main" class="mf-page mf-account-page" tabindex="-1">
        <section class="mf-account-card">
            <p class="mf-eyebrow">Join the conversation</p>
            <h1>{{ $register ? 'Create your account' : 'Welcome back' }}</h1>
            <p class="mf-account-intro">{{ $register ? 'Bring your questions and perspective to the sources you discover.' : 'Sign in to start a discussion or reply to a conversation.' }}</p>
            @if ($errors->any())<div class="mf-form-error" role="alert"><ul>@foreach ($errors->all() as $error)<li>{{ $error }}</li>@endforeach</ul></div>@endif
            <form method="POST" action="{{ route($register ? 'account.register.submit' : 'account.sign-in.submit') }}" class="mf-account-form" data-posting-form>
                @csrf
                <input type="hidden" name="return_to" value="{{ $returnTo }}">
                @if ($register)
                    <label for="account-name">Display name</label><input id="account-name" name="name" autocomplete="name" value="{{ old('name') }}" maxlength="255" required>
                    <label for="account-username">Username</label><input id="account-username" name="username" autocomplete="username" value="{{ old('username') }}" minlength="3" maxlength="40" pattern="[a-z0-9_-]+" aria-describedby="username-hint" required>
                    <p id="username-hint" class="mf-muted">3–40 lowercase letters, numbers, underscores or hyphens.</p>
                @endif
                <label for="account-email">Email</label><input id="account-email" name="email" type="email" autocomplete="{{ $register ? 'email' : 'username' }}" value="{{ old('email') }}" maxlength="255" required>
                <label for="account-password">Password</label><input id="account-password" name="password" type="password" autocomplete="{{ $register ? 'new-password' : 'current-password' }}" @if ($register) minlength="8" @endif maxlength="1024" required>
                @if ($register)<label for="account-confirm">Confirm password</label><input id="account-confirm" name="password_confirmation" type="password" autocomplete="new-password" minlength="8" maxlength="1024" required>@endif
                <button type="submit" class="mf-button mf-primary">{{ $register ? 'Create account' : 'Sign in' }}</button>
            </form>
            @unless ($register)<p class="mf-account-switch"><a class="mf-text-link" href="{{ route('account.password.request', ['return_to' => $returnTo]) }}">Forgot your password?</a></p>@endunless
            <p class="mf-account-switch">{{ $register ? 'Already have an account?' : 'New to MiFolyo?' }} <a class="mf-text-link" href="{{ route($register ? 'account.sign-in' : 'account.register', ['return_to' => $returnTo]) }}">{{ $register ? 'Sign in' : 'Create an account' }}</a></p>
        </section>
    </main>
</x-search-layout>
