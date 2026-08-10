<?php

namespace App\Http\Requests;

use App\Rules\SourceUrlV1;
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
            'url' => ['bail', 'required_if:content_type,link', 'nullable', 'string', new SourceUrlV1],
            'is_nsfw' => ['sometimes', 'boolean'],
        ];
    }
}
