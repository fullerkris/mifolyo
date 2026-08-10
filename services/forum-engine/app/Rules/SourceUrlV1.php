<?php

namespace App\Rules;

use App\Support\SourceUrlNormalizationException;
use App\Support\SourceUrlNormalizer;
use Closure;
use Illuminate\Contracts\Validation\ValidationRule;
use Illuminate\Translation\PotentiallyTranslatedString;

final class SourceUrlV1 implements ValidationRule
{
    /**
     * @param  Closure(string, ?string=): PotentiallyTranslatedString  $fail
     */
    public function validate(string $attribute, mixed $value, Closure $fail): void
    {
        if (! is_string($value)) {
            return;
        }

        try {
            SourceUrlNormalizer::normalizeV1($value);
        } catch (SourceUrlNormalizationException $exception) {
            $fail($exception->reason);
        }
    }
}
