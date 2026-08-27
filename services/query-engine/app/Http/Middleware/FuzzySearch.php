<?php

namespace App\Http\Middleware;

use Closure;
use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Log;
use Symfony\Component\HttpFoundation\Response;

class FuzzySearch
{
    public function handle(Request $request, Closure $next): Response
    {
        $validated = $request->validate([
            'q' => [
                'bail',
                'nullable',
                'string',
                'max:500',
                static function (string $attribute, mixed $value, \Closure $fail): void {
                    if (! mb_check_encoding($value, 'UTF-8')) {
                        $fail('The query must be valid UTF-8.');
                    }
                },
            ],
            'page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ]);
        $query = $validated['q'] ?? null;
        if ($query === null || $query === '') {
            return $next($request);
        }

        // Split the query string
        $query = str_replace('+', ' ', $query);
        $queryWords = preg_split('/\s+/u', mb_strtolower(trim($query), 'UTF-8'), -1, PREG_SPLIT_NO_EMPTY);
        abort_if($queryWords === false, 422, 'The query must be valid UTF-8.');
        abort_if(count($queryWords) > 20, 422, 'Too many search terms.');
        $processedQuery = [];
        $hasSuggestions = false;

        try {
            foreach ($queryWords as $word) {
                if (trim($word) === '') {
                    continue;
                }
                $suggestion = $this->checkOrSuggestWord($word);

                if ($suggestion && $suggestion !== $word) {
                    $processedQuery[] = $suggestion;
                    $hasSuggestions = true;
                } else {
                    $processedQuery[] = $word;
                }
            }

            $processedQueryString = implode(' ', $processedQuery);
            $request->attributes->set('processedQuery', $processedQueryString);
            if ($hasSuggestions) {
                $request->attributes->set('hasSuggestions', true);
            }

        } catch (\Exception) {
            Log::error('Spell-check processing failed.');
        }

        return $next($request);
    }

    private function checkOrSuggestWord(string $word): ?string
    {
        try {
            $collection = DB::connection('mongodb')->table('dictionary');
            // Check DB directly for exact word
            $exists = $collection->find($word);

            if ($exists) {
                return $word;
            }

            $length = mb_strlen($word, 'UTF-8');
            $searchLength = $length - 3 > 0 ? $length - 2 : 1;
            $firstTwoChars = preg_quote(mb_substr($word, 0, $searchLength, 'UTF-8'), '/');

            $cursor = DB::connection('mongodb')
                ->table('dictionary')
                ->raw(function ($collection) use ($firstTwoChars, $length) {
                    return $collection->aggregate([
                        [
                            '$match' => [
                                '_id' => ['$regex' => '^'.$firstTwoChars, '$options' => 'i'],
                            ],
                        ],
                        [
                            '$addFields' => [
                                'length' => ['$strLenCP' => '$_id'],
                            ],
                        ],
                        [
                            '$match' => [
                                'length' => ['$gte' => $length - 1, '$lte' => $length + 1],
                            ],
                        ],
                        ['$limit' => 100],
                    ], ['maxTimeMS' => 250]);
                });

            // Find best match by Levenshtein distance
            $bestMatch = null;
            $minDistance = PHP_INT_MAX;
            $wordLength = mb_strlen($word, 'UTF-8');

            foreach ($cursor as $document) {
                $candidate = $document->_id;
                $candidateLength = mb_strlen($candidate, 'UTF-8');

                if (abs($candidateLength - $wordLength) > 2) {
                    continue; // Too different
                }

                $distance = $this->unicodeLevenshtein($word, $candidate);

                $maxDistance = $wordLength <= 4 ? 1 : min(2, floor($wordLength / 4));
                if ($distance <= $maxDistance && $distance < $minDistance) {
                    $minDistance = $distance;
                    $bestMatch = $candidate;
                }
            }

            return $bestMatch ?? $word;

        } catch (\Exception) {
            Log::warning('Suggestion lookup failed.');

            return $word; // fallback to original word
        }
    }

    private function unicodeLevenshtein(string $left, string $right): int
    {
        $leftCharacters = mb_str_split($left, 1, 'UTF-8');
        $rightCharacters = mb_str_split($right, 1, 'UTF-8');
        $previousRow = range(0, count($rightCharacters));

        foreach ($leftCharacters as $leftIndex => $leftCharacter) {
            $currentRow = [$leftIndex + 1];
            foreach ($rightCharacters as $rightIndex => $rightCharacter) {
                $currentRow[] = min(
                    $currentRow[$rightIndex] + 1,
                    $previousRow[$rightIndex + 1] + 1,
                    $previousRow[$rightIndex] + ($leftCharacter === $rightCharacter ? 0 : 1),
                );
            }
            $previousRow = $currentRow;
        }

        return $previousRow[count($rightCharacters)];
    }
}
