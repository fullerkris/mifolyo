<?php

namespace Tests\Unit;

use App\Support\SearchResultPresenter;
use PHPUnit\Framework\TestCase;

class SearchResultPresenterTest extends TestCase
{
    public function test_highlights_literal_unicode_terms_without_matching_inside_words(): void
    {
        $presenter = new SearchResultPresenter('café C++ art [guide] a.b &');
        $text = 'CAFÉ C++ art article [guide] a.b axb & <script>art</script>';
        $parts = $presenter->parts($text);

        $this->assertSame($text, implode('', array_column($parts, 'text')));
        $this->assertSame(['CAFÉ', 'C++', 'art', '[guide]', 'a.b', 'art'], array_column(array_filter($parts, fn ($part) => $part['match']), 'text'));
        $this->assertSame([['text' => 'Plain text', 'match' => false]], (new SearchResultPresenter('.* &'))->parts('Plain text'));
        $this->assertSame(['検索'], array_column(array_filter((new SearchResultPresenter('検索'))->parts('検索方法について'), fn ($part) => $part['match']), 'text'));
    }

    public function test_excerpt_finds_late_matches_and_retains_unicode_word_context(): void
    {
        $presenter = new SearchResultPresenter('café');
        $summary = str_repeat('Opening material without a match. ', 25).'A useful café guide explains the details. '.str_repeat('Further context follows. ', 20);
        $excerpt = $presenter->excerpt($summary, 'A generic description.');

        $this->assertStringStartsWith('… ', $excerpt);
        $this->assertStringContainsString('A useful café guide explains the details.', $excerpt);
        $this->assertStringEndsWith(' …', $excerpt);
        $this->assertLessThanOrEqual(244, mb_strlen($excerpt));
        $this->assertTrue(mb_check_encoding($excerpt, 'UTF-8'));
    }

    public function test_relevant_description_beats_unrelated_summary_and_summary_wins_ties(): void
    {
        $presenter = new SearchResultPresenter('source quality');
        $this->assertSame('A source quality checklist.', $presenter->excerpt('Welcome to this site.', 'A source quality checklist.'));
        $this->assertSame('A source quality summary.', $presenter->excerpt('A source quality summary.', 'A source quality description.'));
        $this->assertSame('An introductory summary.', $presenter->excerpt('An introductory summary.', 'A description.'));
    }

    public function test_missing_malformed_and_whitespace_metadata_have_predictable_fallbacks(): void
    {
        $presenter = new SearchResultPresenter('example');
        $this->assertSame('Description fallback.', $presenter->excerpt("\n\t\u{00a0}", 'Description fallback.'));
        $this->assertSame('Description fallback.', $presenter->excerpt(['legacy' => 'unexpected'], 'Description fallback.'));
        $this->assertSame('', $presenter->excerpt(null, new \stdClass));
        $this->assertSame('0', $presenter->excerpt('0', 'Other text'));
        $this->assertSame('A useful title', SearchResultPresenter::text(" A\n useful\t title\u{00a0}"));
        $this->assertTrue(mb_check_encoding(SearchResultPresenter::text("Invalid \xff text"), 'UTF-8'));
        $this->assertLessThanOrEqual(12000, mb_strlen(SearchResultPresenter::text(str_repeat('x', 20000))));
    }

    public function test_long_unbroken_metadata_stays_bounded_without_invalid_utf8(): void
    {
        $presenter = new SearchResultPresenter('guide');
        $text = str_repeat('界', 1000);
        $this->assertSame(str_repeat('界', 140).'…', SearchResultPresenter::shorten($text, 140));
        $this->assertSame(str_repeat('界', 240).' …', $presenter->excerpt($text, ''));
        $this->assertSame('Unchanged title', SearchResultPresenter::shorten('Unchanged title', 140));
    }
}
