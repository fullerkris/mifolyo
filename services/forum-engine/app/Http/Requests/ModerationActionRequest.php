<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class ModerationActionRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'target_type' => ['required', Rule::in(['post', 'comment'])],
            'target_id' => ['required', 'integer', 'min:1'],
            'action' => ['required', Rule::in(['remove', 'lock'])],
            'reason' => ['nullable', 'string', 'max:2000'],
            'report_id' => ['nullable', 'integer', 'exists:reports,id'],
        ];
    }
}
