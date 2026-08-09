<?php

namespace App\Support;

use InvalidArgumentException;

final class SourceUrlNormalizationException extends InvalidArgumentException
{
    public function __construct(public readonly string $reason)
    {
        parent::__construct($reason);
    }

    public function errorCode(): string
    {
        return $this->reason;
    }
}
