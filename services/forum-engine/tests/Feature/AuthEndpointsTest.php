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
            'email' => 'kris@example.com',
            'password' => 'password123',
            'password_confirmation' => 'password123',
        ]);

        $response
            ->assertCreated()
            ->assertJsonPath('data.user.email', 'kris@example.com')
            ->assertJsonPath('data.token_type', 'Bearer');

        $this->assertDatabaseHas('users', [
            'email' => 'kris@example.com',
        ]);

        $this->assertNotNull($response->json('data.token'));
    }

    public function test_login_and_me_return_authenticated_user(): void
    {
        User::factory()->create([
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
