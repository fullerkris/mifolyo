<?php

namespace Tests\Feature;

use App\Models\User;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class AuthEndpointsTest extends TestCase
{
    use RefreshDatabase;

    public function test_register_returns_bearer_token_and_user(): void
    {
        $response = $this->postJson('/api/auth/register', [
            'name' => 'Kris Fuller',
            'username' => 'kris_fuller',
            'email' => 'kris@example.com',
            'password' => 'password123',
            'password_confirmation' => 'password123',
        ]);

        $response
            ->assertCreated()
            ->assertJsonPath('data.user.email', 'kris@example.com')
            ->assertJsonPath('data.user.username', 'kris_fuller')
            ->assertJsonPath('data.user.level', 1)
            ->assertJsonPath('data.user.standing', 'healthy')
            ->assertJsonPath('data.token_type', 'Bearer')
            ->assertJsonStructure(['data' => ['expires_at']]);

        $this->assertDatabaseHas('users', [
            'email' => 'kris@example.com',
            'username' => 'kris_fuller',
            'level' => 1,
            'standing' => 'healthy',
        ]);

        $token = $response->json('data.token');
        $this->assertNotNull($token);

        $user = User::query()->where('email', 'kris@example.com')->firstOrFail();
        $this->assertSame(hash('sha256', $token), $user->api_token);
        $this->assertNotSame($token, $user->api_token);
        $this->assertNotNull($user->api_token_expires_at);
    }

    public function test_login_and_me_return_authenticated_user(): void
    {
        $user = User::factory()->create([
            'name' => 'Kris Fuller',
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $loginResponse = $this->postJson('/api/auth/login', [
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $token = $loginResponse->json('data.token');

        $loginResponse
            ->assertOk()
            ->assertJsonPath('data.user.email', 'kris@example.com')
            ->assertJsonPath('data.token_type', 'Bearer');

        $this->getJson('/api/auth/me', [
            'Authorization' => 'Bearer '.$token,
        ])->assertOk()->assertJsonPath('data.email', 'kris@example.com');

        $this->assertNotNull($user->fresh()->api_token_last_used_at);
    }

    public function test_refresh_rotates_bearer_token(): void
    {
        User::factory()->create([
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $loginResponse = $this->postJson('/api/auth/login', [
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $oldToken = $loginResponse->json('data.token');

        $refreshResponse = $this->postJson('/api/auth/refresh', [], [
            'Authorization' => 'Bearer '.$oldToken,
        ]);

        $refreshResponse
            ->assertOk()
            ->assertJsonPath('data.token_type', 'Bearer')
            ->assertJsonStructure(['data' => ['token', 'expires_at']]);

        $newToken = $refreshResponse->json('data.token');
        $this->assertNotSame($oldToken, $newToken);

        $this->app['auth']->forgetGuards();

        $this->getJson('/api/auth/me', [
            'Authorization' => 'Bearer '.$oldToken,
        ])->assertUnauthorized();

        $this->app['auth']->forgetGuards();

        $this->getJson('/api/auth/me', [
            'Authorization' => 'Bearer '.$newToken,
        ])->assertOk();
    }

    public function test_logout_revokes_current_token(): void
    {
        User::factory()->create([
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $loginResponse = $this->postJson('/api/auth/login', [
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $token = $loginResponse->json('data.token');

        $this->postJson('/api/auth/logout', [], [
            'Authorization' => 'Bearer '.$token,
        ])->assertOk()->assertJsonPath('message', 'Logged out.');

        $this->app['auth']->forgetGuards();

        $this->getJson('/api/auth/me', [
            'Authorization' => 'Bearer '.$token,
        ])->assertUnauthorized();

        $user = User::query()->where('email', 'kris@example.com')->firstOrFail();
        $this->assertNull($user->api_token);
        $this->assertNull($user->api_token_expires_at);
        $this->assertNull($user->api_token_last_used_at);
    }

    public function test_expired_token_is_rejected_and_cleared(): void
    {
        $token = 'expired-token';
        $user = User::factory()->create();
        $user->forceFill([
            'api_token' => hash('sha256', $token),
            'api_token_expires_at' => now()->subMinute(),
        ])->save();

        $this->getJson('/api/auth/me', [
            'Authorization' => 'Bearer '.$token,
        ])->assertUnauthorized()->assertJsonPath('message', 'Token expired.');

        $this->assertNull($user->fresh()->api_token);
    }

    public function test_api_auth_failure_returns_json_without_accept_header(): void
    {
        $this->get('/api/auth/me')
            ->assertUnauthorized()
            ->assertJsonPath('message', 'Unauthenticated.');
    }

    public function test_login_with_invalid_credentials_returns_error(): void
    {
        User::factory()->create([
            'email' => 'kris@example.com',
            'password' => 'password123',
        ]);

        $this->postJson('/api/auth/login', [
            'email' => 'kris@example.com',
            'password' => 'wrong-password',
        ])->assertStatus(422)->assertJsonPath('message', 'Invalid credentials.');
    }
}
