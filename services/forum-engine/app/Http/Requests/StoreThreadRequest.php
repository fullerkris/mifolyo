<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;

class StoreThreadRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'community_slug' => ['required', 'string', 'exists:communities,slug'],
            'title' => ['required', 'string', 'max:300'],
            'body' => ['nullable', 'string', 'max:50000'],
            'source_url' => ['required', 'url:http,https', 'max:2048'],
            'is_nsfw' => ['sometimes', 'boolean'],
        ];
    }
}
