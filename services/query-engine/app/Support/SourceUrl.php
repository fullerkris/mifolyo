<?php

namespace App\Support;

class SourceUrl
{
    public static function http(mixed $value, bool $allowLegacy = false): ?string
    {
        if (! is_string($value) || $value === '' || strlen($value) > 2048 || preg_match('/[\\x00-\\x20\\x7f]/', $value)) {
            return null;
        }
        if (str_contains($value, '\\')) {
            return null;
        }
        if ($allowLegacy && ! str_starts_with($value, '/') && ! preg_match('/^[a-z][a-z0-9+.-]*:/i', $value)) {
            $value = 'https://'.$value;
        }

        return strlen($value) <= 2048 && filter_var($value, FILTER_VALIDATE_URL)
            && in_array(strtolower(parse_url($value, PHP_URL_SCHEME) ?? ''), ['http', 'https'], true) ? $value : null;
    }
}
