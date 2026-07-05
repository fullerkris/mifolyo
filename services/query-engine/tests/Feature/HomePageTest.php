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
            ->assertSee('/api/search')
            ->assertSee('Search the web and community...');
    }
}
