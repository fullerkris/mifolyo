<?php

namespace App\Support;

use InvalidArgumentException;

class SourceUrlNormalizer
{
    /**
     * Normalize source URLs so equivalent search-result URLs share one lookup key.
     * Fragments are intentionally dropped because they do not identify a distinct page.
     *
     * @return array{source_url: string, source_url_hash: string, source_domain: string, source_path: string}
     */
    public static function normalize(string $url): array
    {
        $parts = parse_url(trim($url));

        if (! is_array($parts) || empty($parts['scheme']) || empty($parts['host'])) {
            throw new InvalidArgumentException('A valid absolute URL is required.');
        }

        $scheme = strtolower($parts['scheme']);
        if (! in_array($scheme, ['http', 'https'], true)) {
            throw new InvalidArgumentException('Only HTTP and HTTPS URLs are supported.');
        }

        $host = strtolower($parts['host']);
        $port = isset($parts['port']) ? (int) $parts['port'] : null;
        $portPart = '';

        if ($port && ! (($scheme === 'http' && $port === 80) || ($scheme === 'https' && $port === 443))) {
            $portPart = ':'.$port;
        }

        $path = $parts['path'] ?? '/';
        if ($path === '') {
            $path = '/';
        }

        $query = isset($parts['query']) && $parts['query'] !== '' ? '?'.$parts['query'] : '';
        $sourcePath = $path.$query;
        $sourceUrl = $scheme.'://'.$host.$portPart.$sourcePath;

        return [
            'source_url' => $sourceUrl,
            'source_url_hash' => hash('sha256', $sourceUrl),
            'source_domain' => $host,
            'source_path' => $sourcePath,
        ];
    }
}
