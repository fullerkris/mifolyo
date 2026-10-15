<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Route;
use MongoDB\Driver\Exception\ConnectionTimeoutException;
use Tests\TestCase;

class SearchUiTest extends TestCase
{
    private function results(array $overrides = []): void
    {
        Route::get('/ui-test-results', fn () => view('search-results', array_merge([
            'query' => 'alpha',
            'originalQuery' => 'alpha',
            'suggestions' => false,
            'page' => 1,
            'total' => 1,
            'topImages' => [],
            'results' => [['_id' => 'https://example.test/article?a=1&b=two', 'title' => 'A useful source', 'summary_text' => 'An indexed excerpt.']],
        ], $overrides)));
    }

    public function test_search_cards_render_real_source_and_discussion_links_and_escape_content(): void
    {
        $source = 'https://example.test/article?a=1&b=two';
        $this->results(['results' => [['_id' => $source, 'title' => '<script>bad()</script>', 'description' => '<img src=x onerror=bad()>']]]);

        $this->get('/ui-test-results')->assertOk()
            ->assertSee('href="'.e($source).'"', false)
            ->assertSee('data-discuss-url="'.e($source).'"', false)
            ->assertSee(e(route('discussions', ['url' => $source, 'title' => '<script>bad()</script>'])), false)
            ->assertSee('&lt;script&gt;bad()&lt;/script&gt;', false)
            ->assertDontSee('<script>bad()', false)
            ->assertDontSee('<img src=x', false)
            ->assertDontSee('High Trust')
            ->assertDontSee('Sample result');
    }

    public function test_missing_titles_and_non_http_index_entries_do_not_make_unsafe_links(): void
    {
        $this->results(['results' => [
            ['_id' => 'https://example.test/source', 'title' => '', 'summary_text' => ''],
            ['_id' => 'javascript:alert(1)', 'title' => 'Broken entry'],
        ]]);
        $this->get('/ui-test-results')->assertOk()->assertSee('example.test')
            ->assertSee('No excerpt is available.')
            ->assertSee('Source URL unavailable')->assertDontSee('href="javascript:', false);
    }

    public function test_highlighting_escapes_indexed_markup_and_uses_the_effective_query(): void
    {
        $this->results(['query' => 'café', 'originalQuery' => 'caffé', 'suggestions' => true, 'results' => [[
            '_id' => 'https://example.test/caf%C3%A9?x=1&b=two',
            'title' => '<script>alert("CAFÉ")</script>',
            'summary_text' => 'An unrelated summary.',
            'description' => '<img src=x onerror=bad()> A café description; caffé was the typo.',
        ]]]);
        $this->get('/ui-test-results')->assertOk()
            ->assertSee('<mark class="mf-result-match">CAFÉ</mark>', false)
            ->assertSee('<mark class="mf-result-match">café</mark>', false)
            ->assertDontSee('<mark class="mf-result-match">caffé</mark>', false)
            ->assertSee('&lt;script&gt;', false)->assertSee('&lt;img src=x', false)
            ->assertDontSee('<script>alert(', false)->assertDontSee('<img src=x', false)
            ->assertDontSee('An unrelated summary.');
    }

    public function test_long_metadata_is_expandable_and_source_links_keep_exact_url_identity(): void
    {
        $source = 'https://EXAMPLE.test:443/'.str_repeat('long-path/', 20).'?b=two&a=1#section';
        $title = str_repeat('A long indexed title with useful detail ', 12);
        $this->results(['results' => [['_id' => $source, 'title' => $title, 'summary_text' => ['invalid' => 'shape'], 'description' => 'A usable description.']]]);
        $this->get('/ui-test-results')->assertOk()
            ->assertSee('Full title and URL')->assertSee('A usable description.')
            ->assertSee('class="mf-result-full-title"', false)->assertSee(trim($title))
            ->assertSee('href="'.e($source).'"', false)->assertSee('data-discuss-url="'.e($source).'"', false)
            ->assertSee('EXAMPLE.test:443')->assertSee('dir="ltr"', false);
    }

    public function test_malformed_and_zero_metadata_do_not_create_warnings_or_fake_text(): void
    {
        $this->results(['results' => [
            ['_id' => 'https://example.test/empty', 'title' => [], 'summary_text' => new \stdClass, 'description' => " \n\t"],
            ['_id' => null, 'title' => null],
            ['_id' => 'https://example.test/zero', 'title' => '0', 'summary_text' => '0'],
            ['_id' => 'https://example.test/'.str_repeat('x', 2048), 'title' => 'Oversized source'],
        ]]);
        $this->get('/ui-test-results')->assertOk()->assertSee('example.test')->assertSee('Untitled page')
            ->assertSee('No excerpt is available.')->assertSee('No excerpt is available for this indexed entry.')
            ->assertDontSee('>Array<', false)->assertDontSee('data-discuss-url="https://example.test/'.str_repeat('x', 2048), false);
    }

    public function test_pagination_encodes_query_and_reaches_pages_past_ten(): void
    {
        $query = 'C++ & café #1';
        $this->results(['query' => $query, 'originalQuery' => $query, 'page' => 12, 'total' => 700]);
        $response = $this->get('/ui-test-results?'.http_build_query(['q' => $query, 'page' => 12]));
        $response->assertOk()
            ->assertSee('q=C%2B%2B%20%26%20caf%C3%A9%20%231&amp;page=13', false)
            ->assertSee('aria-current="page" aria-label="Page 12"', false)
            ->assertSee('aria-label="Page 35"', false)
            ->assertDontSee('page=36', false);
    }

    public function test_empty_and_single_page_results_have_no_phantom_pages(): void
    {
        $this->results(['results' => [], 'total' => 0]);
        $this->get('/ui-test-results')->assertOk()->assertSee('No results found')
            ->assertDontSee('aria-label="Search result pages"', false);
        $this->results();
        $this->get('/ui-test-results')->assertOk()->assertDontSee('aria-label="Search result pages"', false);
    }

    public function test_pagination_never_links_beyond_the_backend_page_limit(): void
    {
        $this->results(['total' => 9999, 'page' => 100]);
        $this->get('/ui-test-results?q=alpha&page=100')->assertOk()
            ->assertSee('aria-label="Page 100"', false)
            ->assertDontSee('page=101', false)->assertDontSee('rel="next"', false);
    }

    public function test_spelling_opt_out_uses_original_query_and_exact_search(): void
    {
        $this->results(['query' => 'corrected', 'originalQuery' => 'orig & typo', 'suggestions' => true]);
        $this->get('/ui-test-results')->assertOk()
            ->assertSee(e(route('search.exact', ['q' => 'orig & typo'])), false)
            ->assertSee('value="orig &amp; typo"', false)
            ->assertDontSee('processed_query=');
    }

    public function test_html_search_validation_is_actionable_and_json_stays_json(): void
    {
        $this->get('/search?q[]=bad')->assertStatus(422)->assertSee('Let’s adjust that search');
        $this->getJson('/search?q[]=bad')->assertUnprocessable()->assertJsonValidationErrors('q');
        $this->get('/search/exact?q=alpha&page=0')->assertStatus(422)->assertSee('page between 1 and 100');
    }

    public function test_index_failure_keeps_query_and_retry_without_exposing_exception_details(): void
    {
        DB::shouldReceive('connection')->with('mongodb')->andThrow(new ConnectionTimeoutException('private database address'));
        $query = 'alpha & beta';
        $this->get('/search/exact?'.http_build_query(['q' => $query, 'page' => 2]))
            ->assertStatus(503)->assertSee('Search is temporarily unavailable')
            ->assertSee('value="alpha &amp; beta"', false)
            ->assertSee(e(route('search.exact', ['q' => $query, 'page' => 2])), false)
            ->assertDontSee('private database address');
    }

    public function test_rate_limit_preserves_retry_after_and_gives_readable_html(): void
    {
        Route::get('/search', fn () => abort(429, '', ['Retry-After' => '60']));
        $this->get('/search?q=alpha')->assertStatus(429)->assertHeader('Retry-After', '60')
            ->assertSee('A moment to catch up');
    }
}
