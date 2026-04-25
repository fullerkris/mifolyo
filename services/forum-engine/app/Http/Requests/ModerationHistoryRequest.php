<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class ModerationHistoryRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'community_id' => ['sometimes', 'integer', 'min:1', 'exists:communities,id'],
            'action' => ['sometimes', Rule::in(['remove', 'lock'])],
            'target_type' => ['sometimes', Rule::in(['post', 'comment'])],
            'per_page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ];
    }
}
