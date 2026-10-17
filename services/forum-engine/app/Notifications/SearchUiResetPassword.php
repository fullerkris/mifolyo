<?php

namespace App\Notifications;

use Illuminate\Auth\Notifications\ResetPassword;

class SearchUiResetPassword extends ResetPassword
{
    public function __construct(#[\SensitiveParameter] string $token, private string $baseUrl, private string $returnTo)
    {
        parent::__construct($token);
    }

    protected function resetUrl($notifiable): string
    {
        return $this->baseUrl.'/reset-password?'.http_build_query([
            'token' => $this->token,
            'email' => $notifiable->getEmailForPasswordReset(),
            'return_to' => $this->returnTo,
        ], '', '&', PHP_QUERY_RFC3986);
    }
}
