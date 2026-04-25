<?php

return [

    /*
    |--------------------------------------------------------------------------
    | Third Party Services
    |--------------------------------------------------------------------------
    |
    | This file is for storing the credentials for third party services such
    | as Mailgun, Postmark, AWS and more. This file provides the de facto
    | location for this type of information, allowing packages to have
    | a conventional file to locate the various service credentials.
    |
    */

    'postmark' => [
        'key' => env('POSTMARK_API_KEY'),
    ],

    'resend' => [
        'key' => env('RESEND_API_KEY'),
    ],

    'ses' => [
        'key' => env('AWS_ACCESS_KEY_ID'),
        'secret' => env('AWS_SECRET_ACCESS_KEY'),
        'region' => env('AWS_DEFAULT_REGION', 'us-east-1'),
    ],

    'slack' => [
        'notifications' => [
            'bot_user_oauth_token' => env('SLACK_BOT_USER_OAUTH_TOKEN'),
            'channel' => env('SLACK_BOT_USER_DEFAULT_CHANNEL'),
        ],
    ],

    'search' => [
        'base_url' => env('SEARCH_API_BASE_URL', 'http://localhost:8080'),
        'timeout_seconds' => (float) env('SEARCH_API_TIMEOUT_SECONDS', 3),
        'connect_timeout_seconds' => (float) env('SEARCH_API_CONNECT_TIMEOUT_SECONDS', 1.5),
        'retry_attempts' => (int) env('SEARCH_API_RETRY_ATTEMPTS', 2),
        'retry_delay_ms' => (int) env('SEARCH_API_RETRY_DELAY_MS', 150),
    ],

];
