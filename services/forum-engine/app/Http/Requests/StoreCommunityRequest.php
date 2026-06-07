<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;

class StoreCommunityRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'name' => ['required', 'string', 'max:120', 'unique:communities,name'],
            'description' => ['nullable', 'string', 'max:4000'],
            'is_private' => ['sometimes', 'boolean'],
            'is_nsfw' => ['sometimes', 'boolean'],
        ];
    }
}
