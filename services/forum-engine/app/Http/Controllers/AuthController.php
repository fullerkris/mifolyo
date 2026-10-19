<?php

namespace App\Http\Controllers;

use App\Http\Requests\AuthLoginRequest;
use App\Http\Requests\AuthRegisterRequest;
use App\Models\User;
use Illuminate\Http\JsonResponse;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
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

        return DB::transaction(function () use ($credentials): JsonResponse {
            // Reset/deletion must not be followed by a token issued from a stale password.
            $user = User::query()->where('email', $credentials['email'])->lockForUpdate()->first();

            if (! $user || ! Hash::check($credentials['password'], $user->password)) {
                return response()->json([
                    'message' => 'Invalid credentials.',
                ], 422);
            }

            return response()->json($this->authenticatedPayload($user));
        }, 3);
    }

    public function me(Request $request): JsonResponse
    {
        return response()->json([
            'data' => $request->user(),
        ]);
    }

    public function logout(Request $request): JsonResponse
    {
        User::query()->whereKey($request->user('api')->id)
            ->where('api_token', hash('sha256', $request->bearerToken() ?? ''))->update([
                'api_token' => null,
                'api_token_expires_at' => null,
                'api_token_last_used_at' => null,
            ]);

        return response()->json([
            'message' => 'Logged out.',
        ]);
    }

    public function refresh(Request $request): JsonResponse
    {
        return DB::transaction(function () use ($request): JsonResponse {
            $user = User::query()->lockForUpdate()->find($request->user('api')->id);
            abort_unless($user && $user->api_token && $request->bearerToken()
                && hash_equals($user->api_token, hash('sha256', $request->bearerToken()))
                && ! $user->api_token_expires_at?->isPast(), 401);

            return response()->json($this->authenticatedPayload($user));
        }, 3);
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
