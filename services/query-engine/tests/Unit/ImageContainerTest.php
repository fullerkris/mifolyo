<?php

namespace Tests\Unit;

use Tests\TestCase;

class ImageContainerTest extends TestCase
{
    public function test_image_urls_preserve_absolute_urls_and_support_legacy_scheme_less_urls(): void
    {
        $cases = [
            ['http://example.com/page', 'http://cdn.example.com/image.jpg'],
            ['https://example.com/page', 'https://cdn.example.com/image.jpg'],
            ['example.com/page', 'cdn.example.com/image.jpg'],
        ];

        foreach ($cases as [$pageUrl, $imageUrl]) {
            $html = view('components.image-container', [
                'page_url' => $pageUrl,
                'url' => $imageUrl,
                'alt' => '',
                'title' => '',
                'text' => '',
            ])->render();

            $this->assertStringContainsString('href="'.(str_contains($pageUrl, '://') ? $pageUrl : 'https://'.$pageUrl).'"', $html);
            $this->assertStringContainsString('src="'.(str_contains($imageUrl, '://') ? $imageUrl : 'https://'.$imageUrl).'"', $html);
        }
    }

    public function test_random_article_link_preserves_absolute_url_and_supports_legacy_url(): void
    {
        $this->withoutVite();

        foreach (['https://example.com/page', 'http://example.com/page', 'example.com/page'] as $url) {
            $html = view('cringe-results', [
                'totalSearches' => 0,
                'randomPage' => [
                    'url' => $url,
                    'title' => 'Example',
                    'summary_text' => null,
                ],
            ])->render();

            $expected = str_contains($url, '://') ? $url : 'https://'.$url;
            $this->assertStringContainsString('href="'.$expected.'"', $html);
        }
    }
}
