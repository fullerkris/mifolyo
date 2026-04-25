<?php

namespace App\Http\Requests;

use Illuminate\Foundation\Http\FormRequest;
use Illuminate\Validation\Rule;

class StoreVoteRequest extends FormRequest
{
    public function authorize(): bool
    {
        return true;
    }

    public function rules(): array
    {
        return [
            'votable_type' => ['required', Rule::in(['post', 'comment'])],
            'votable_id' => ['required', 'integer', 'min:1'],
            'value' => ['required', 'integer', Rule::in([-1, 1])],
        ];
    }
}
