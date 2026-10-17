<?php

namespace Tests\Feature;

use App\Services\ForumApi;
use App\Support\SearchContext;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Route;
use MongoDB\Driver\Exception\ConnectionTimeoutException;
use Tests\TestCase;

class DiscoveryUiTest extends TestCase
{
    public function test_secondary_views_support_direct_entry_without_database_access(): void
    {
        DB::shouldReceive('connection')->never();
        $this->get('/images')->assertOk()->assertSee('Start with an image search')->assertSee('action="'.route('search.images').'"', false);
        $this->get('/page-links')->assertOk()->assertSee('Every page is part of a bigger picture.')->assertDontSee('data-map-canvas', false);
    }

    public function test_invalid_inputs_have_editable_html_and_keep_json_validation(): void
    {
        foreach (['/images?q[]=bad', '/images?q=%FF', '/images?page=0', '/images?'.http_build_query(['q' => str_repeat('term ', 21)])] as $url) {
            $this->get($url)->assertStatus(422)->assertSee('Let’s adjust that search')->assertSee('Search images');
        }
        foreach (['url[]=bad', 'url=javascript:alert(1)', 'url=https://example.test&direction[]=incoming', 'url=https://example.test&page=101'] as $query) {
            $this->get('/page-links?'.$query)->assertStatus(422)->assertSee('Let’s adjust that page URL')->assertSee('Explore links');
        }
        $this->getJson('/page-links?url=ftp://example.test')->assertUnprocessable()->assertJsonValidationErrors('url');
        $this->getJson('/images?q[]=bad')->assertUnprocessable()->assertJsonValidationErrors('q');
    }

    public function test_outage_retry_preserves_image_query_and_page_link_source_direction_page_and_origin(): void
    {
        DB::shouldReceive('connection')->with('mongodb')->andThrow(new ConnectionTimeoutException('private database address'));
        $from = '/search/exact?q=C%2B%2B%20%26%20caf%C3%A9&page=2';
        $images = ['q' => 'C++ & café', 'page' => 2, 'from' => $from];
        $this->get('/images?'.http_build_query($images))->assertStatus(503)
            ->assertSee(e(route('search.images', $images)), false)->assertSee('value="C++ &amp; café"', false)
            ->assertDontSee('private database address');
        $links = ['url' => 'https://EXAMPLE.test:443/path?b=two&a=1#section', 'direction' => 'incoming', 'page' => 2, 'from' => $from];
        $this->get('/page-links?'.http_build_query($links))->assertStatus(503)
            ->assertSee('Page links are temporarily unavailable')->assertSee(e(route('page.links', $links)), false)
            ->assertSee('value="'.e($links['url']).'"', false)->assertSee(e($from).'#mf-return', false)
            ->assertDontSee('private database address');
    }

    public function test_rate_limited_secondary_views_keep_retry_after_and_correct_routes(): void
    {
        foreach (['/images' => 'search.images', '/page-links' => 'page.links'] as $path => $route) {
            Route::get($path, fn () => abort(429, '', ['Retry-After' => '60']));
            $this->get($path.'?q=alpha&url=https://example.test')->assertStatus(429)->assertHeader('Retry-After', '60')
                ->assertSee('A moment to catch up')->assertSee(route($route), false)->assertSee('Try again');
        }
    }

    public function test_image_return_preserves_one_validated_web_origin_through_discussions_and_sign_in(): void
    {
        $web = '/search/exact?q=C%2B%2B%20%26%20caf%C3%A9&page=2';
        $images = '/images?'.http_build_query(['q' => 'C++ & café', 'page' => 2, 'from' => $web], '', '&', PHP_QUERY_RFC3986);
        $this->assertSame($images, SearchContext::url($images));
        $this->assertSame('/images?q=alpha', SearchContext::url('/images?q=alpha&from='.urlencode($images)));
        $this->assertSame('/images?q=alpha', SearchContext::url('/images?q=alpha&from=https://evil.test'));
        $this->assertSame($images.'#mf-return', ForumApi::returnTo($images.'#mf-return'));
        $links = '/page-links?'.http_build_query(['url' => 'https://example.test', 'from' => $images]);
        $this->assertSame($links, ForumApi::returnTo($links));
        $this->get('/discussions?'.http_build_query(['url' => 'https://example.test', 'from' => $images]))
            ->assertOk()->assertSee('href="'.e($images).'#mf-return"', false);
        $this->get('/sign-in?'.http_build_query(['return_to' => $links]))->assertOk()->assertSee('value="'.e($links).'"', false);
    }

    public function test_browsing_images_cannot_exhaust_the_web_search_or_page_link_budget(): void
    {
        for ($number = 0; $number < 15; $number++) {
            $this->get('/images')->assertOk();
        }
        $this->get('/images')->assertStatus(429)->assertHeader('Retry-After');
        $this->get('/search/exact')->assertOk();
        $this->get('/page-links')->assertOk();
    }
}
