<?php

namespace Tests\Feature;

use Tests\TestCase;

class DiscussionsPageTest extends TestCase
{
    public function test_discussions_page_renders_for_valid_source_url(): void
    {
        $response = $this->get('/discussions?url='.urlencode('https://example.com/source').'&title='.urlencode('Useful Source'));

        $response
            ->assertOk()
            ->assertSee('Useful Source')
            ->assertSee('https://example.com/source')
            ->assertSee('/api/threads/by-url')
            ->assertSee('/api/threads');
    }

    public function test_discussions_page_requires_http_url(): void
    {
        $this->get('/discussions?url='.urlencode('ftp://example.com/source'))
            ->assertSessionHasErrors('url');
    }
}
