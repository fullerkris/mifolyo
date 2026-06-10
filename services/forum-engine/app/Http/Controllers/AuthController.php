<?php

namespace App\Http\Controllers;

use App\Http\Requests\AuthLoginRequest;
use App\Http\Requests\AuthRegisterRequest;
use App\Models\User;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\Hash;
use Illuminate\Support\Str;

class AuthController extends Controller
{
    public function register(AuthRegisterRequest $request): JsonResponse
    {
        $validated = $request->validated();

        $user = User::create([
            'name' => $validated['name'],
            'username' => $validated['username'],
            'email' => $validated['email'],
            'password' => $validated['password'],
        ]);

        return response()->json($this->authenticatedPayload($user), 201);
    }

    public function login(AuthLoginRequest $request): JsonResponse
    {
        $credentials = $request->validated();

        $user = User::query()->where('email', $credentials['email'])->first();

        if (! $user || ! Hash::check($credentials['password'], $user->password)) {
            return response()->json([
                'message' => 'Invalid credentials.',
            ], 422);
        }

        return response()->json($this->authenticatedPayload($user));
    }

    public function me(Request $request): JsonResponse
    {
        return response()->json([
            'data' => $request->user(),
        ]);
    }

    public function logout(Request $request): JsonResponse
    {
        $request->user('api')->forceFill([
            'api_token' => null,
            'api_token_expires_at' => null,
            'api_token_last_used_at' => null,
        ])->save();

        return response()->json([
            'message' => 'Logged out.',
        ]);
    }

    public function refresh(Request $request): JsonResponse
    {
        return response()->json($this->authenticatedPayload($request->user('api')));
    }

    private function authenticatedPayload(User $user): array
    {
        $token = $this->issueToken($user);

        return [
            'data' => [
                'user' => $user->fresh(),
                'token' => $token,
                'token_type' => 'Bearer',
                'expires_at' => $user->api_token_expires_at?->toJSON(),
            ],
        ];
    }

    private function issueToken(User $user): string
    {
        $token = Str::random(60);
        $expiresAt = now()->addMinutes(config('auth.api_token_ttl_minutes'));

        $user->forceFill([
            'api_token' => hash('sha256', $token),
            'api_token_expires_at' => $expiresAt,
            'api_token_last_used_at' => null,
        ])->save();

        return $token;
    }
}
