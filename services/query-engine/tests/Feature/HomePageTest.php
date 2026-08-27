<?php

namespace Tests\Feature;

use Tests\TestCase;

class HomePageTest extends TestCase
{
    public function test_homepage_renders_mifolyo_search_experience(): void
    {
        $response = $this->get('/');

        $response
            ->assertOk()
            ->assertSee('MiFolyo')
            ->assertSee('Search-first.')
            ->assertSee('Community-powered.')
            ->assertSee(route('home.explore'))
            ->assertSee('Search indexed pages...')
            ->assertSee('type="hidden" name="mode" value="search"', false)
            ->assertSee('type="search"', false)
            ->assertDontSee('name="mode" value="community"', false)
            ->assertDontSee('Paste a page URL to view discussions...')
            ->assertDontSee('href="/api/search"', false);
    }

    public function test_search_mode_sends_keywords_to_web_search(): void
    {
        $this->get('/explore?'.http_build_query([
            'mode' => 'search',
            'q' => 'community search',
        ]))->assertRedirect('/api/search?q=community%20search');
    }

    public function test_community_mode_opens_discussions_for_a_page_url(): void
    {
        $sourceUrl = 'https://example.com/articles/source-quality?ref=home';

        $this->get('/explore?'.http_build_query([
            'mode' => 'community',
            'q' => $sourceUrl,
        ]))->assertRedirect(route('discussions', ['url' => $sourceUrl]));
    }

    public function test_community_mode_error_cannot_change_the_homepage_to_url_input(): void
    {
        $response = $this
            ->from('/')
            ->get('/explore?'.http_build_query([
                'mode' => 'community',
                'q' => 'not-a-url',
            ]));

        $response
            ->assertRedirect('/')
            ->assertSessionHasErrors(['q' => 'Enter a valid HTTP or HTTPS page URL.']);

        $this->get('/')
            ->assertOk()
            ->assertSee('value="not-a-url"', false)
            ->assertSee('type="hidden" name="mode" value="search"', false)
            ->assertSee('type="search"', false)
            ->assertDontSee('name="mode" value="community"', false)
            ->assertSee('Enter a valid HTTP or HTTPS page URL.');
    }

    public function test_invalid_array_inputs_cannot_break_the_homepage(): void
    {
        $this->from('/')
            ->get('/explore?mode[]=community&q[]=https%3A%2F%2Fexample.com')
            ->assertRedirect('/')
            ->assertSessionHasErrors(['mode', 'q']);

        $this->get('/')
            ->assertOk()
            ->assertSee('data-mode="search"', false)
            ->assertSee('value=""', false);
    }
}
