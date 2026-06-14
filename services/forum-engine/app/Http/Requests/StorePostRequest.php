<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class StorePostRequest extends FormRequest
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
            'content_type' => ['sometimes', Rule::in(['text', 'link'])],
            'body' => ['nullable', 'string', 'max:50000', 'required_if:content_type,text'],
            'url' => ['nullable', 'url:http,https', 'max:2048', 'required_if:content_type,link'],
            'is_nsfw' => ['sometimes', 'boolean'],
        ];
    }
}
