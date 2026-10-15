<?php

namespace App\Support;

class SearchResultPresenter
{
    private ?string $pattern;

    public function __construct(string $query)
    {
        $terms = preg_split('/\s+/u', mb_substr(trim($query), 0, 500), -1, PREG_SPLIT_NO_EMPTY) ?: [];
        $terms = array_values(array_unique(array_filter(array_slice($terms, 0, 20), fn ($term) => preg_match('/[\p{L}\p{N}]/u', $term))));
        usort($terms, fn ($left, $right) => mb_strlen($right) <=> mb_strlen($left));
        $patterns = array_map(function ($term) {
            $literal = preg_quote($term, '~');

            // CJK terms can occur in text without spaces; other terms must not match inside words.
            return preg_match('/[\p{Han}\p{Hiragana}\p{Katakana}\p{Hangul}]/u', $term)
                ? $literal : '(?<![\p{L}\p{M}\p{N}_])'.$literal.'(?![\p{L}\p{M}\p{N}_])';
        }, $terms);
        $this->pattern = $patterns === [] ? null : '~('.implode('|', $patterns).')~iu';
    }

    public static function text(mixed $value, int $limit = 12000): string
    {
        if (! is_string($value)) {
            return '';
        }
        $value = mb_substr(mb_convert_encoding($value, 'UTF-8', 'UTF-8'), 0, $limit);

        return trim(preg_replace('/[\p{Z}\s\x00-\x1f\x7f]+/u', ' ', $value) ?? '');
    }

    public static function shorten(string $text, int $limit): string
    {
        if (mb_strlen($text) <= $limit) {
            return $text;
        }
        $short = mb_substr($text, 0, $limit);
        $space = mb_strrpos($short, ' ');
        if ($space !== false && $space >= $limit - 40) {
            $short = mb_substr($short, 0, $space);
        }

        return rtrim($short).'…';
    }

    /** Plain-text segments only: Blade escapes every segment before adding its own mark element. */
    public function parts(string $text): array
    {
        $segments = $this->pattern === null ? [$text] : preg_split($this->pattern, $text, -1, PREG_SPLIT_DELIM_CAPTURE);
        $parts = [];
        foreach ($segments ?: [$text] as $index => $segment) {
            if ($segment !== '') {
                $parts[] = ['text' => $segment, 'match' => $index % 2 === 1];
            }
        }

        return $parts;
    }

    public function excerpt(mixed $summary, mixed $description): string
    {
        $best = '';
        $bestScore = -1;
        foreach ([$summary, $description] as $value) {
            $text = self::text($value);
            if ($text === '') {
                continue;
            }
            $excerpt = $this->window($text);
            $matches = [];
            if ($this->pattern !== null) {
                preg_match_all($this->pattern, $excerpt, $matches);
            }
            $score = count(array_unique(array_map(fn ($match) => mb_strtolower($match), $matches[0] ?? [])));
            // Prefer the summary on ties, but let a relevant description replace unrelated page text.
            if ($score > $bestScore) {
                $best = $excerpt;
                $bestScore = $score;
            }
        }

        return $best;
    }

    private function window(string $text): string
    {
        $length = mb_strlen($text);
        if ($length <= 240) {
            return $text;
        }
        $start = 0;
        if ($this->pattern !== null && preg_match($this->pattern, $text, $match, PREG_OFFSET_CAPTURE)) {
            $position = mb_strlen(substr($text, 0, $match[0][1]));
            $start = max(0, $position - 80);
            if ($start > 0) {
                $space = mb_strpos($text, ' ', $start);
                if ($space !== false && $space < $position) {
                    $start = $space + 1;
                }
            }
        }
        $excerpt = mb_substr($text, $start, 240);
        $hasMore = $start + mb_strlen($excerpt) < $length;
        if ($hasMore) {
            $space = mb_strrpos($excerpt, ' ');
            if ($space !== false && $space >= 200) {
                $excerpt = mb_substr($excerpt, 0, $space);
            }
        }

        return ($start > 0 ? '… ' : '').trim($excerpt).($hasMore ? ' …' : '');
    }
}
