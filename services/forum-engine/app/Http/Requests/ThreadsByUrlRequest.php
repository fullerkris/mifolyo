<?php

namespace App\Http\Requests;

use App\Rules\SourceUrlV1;
use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class ThreadsByUrlRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'url' => ['bail', 'required', 'string', new SourceUrlV1],
            'sort' => ['sometimes', Rule::in(['top', 'new'])],
            'per_page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ];
    }
}
