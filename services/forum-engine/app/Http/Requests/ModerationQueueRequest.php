<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class ModerationQueueRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'status' => ['sometimes', Rule::in(['open', 'resolved', 'dismissed'])],
            'per_page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ];
    }
}
