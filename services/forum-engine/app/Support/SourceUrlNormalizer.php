<?php

namespace App\Support;

final class SourceUrlNormalizer
{
    public const int CANONICALIZATION_VERSION = 1;

    public const int VERSION = self::CANONICALIZATION_VERSION;

    public const int MAX_URL_BYTES = 2048;

    public const string URL_ID_NAMESPACE = "mifolyo-url:v1\0";

    private const string PATH_SAFE_CHARACTERS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~!$&'()*+,;=:@/";

    private const string QUERY_SAFE_CHARACTERS = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~!$&'()*+,;=:@/?";

    /**
     * Preserve the original forum-facing API while using the V1 identity contract.
     *
     * @return array{source_url: string, source_url_hash: string, source_domain: string, source_path: string, source_url_canonicalization_version: int}
     */
    public static function normalize(string $url): array
    {
        return self::normalizeV1($url);
    }

    /**
     * @return array{source_url: string, source_url_hash: string, source_domain: string, source_path: string, source_url_canonicalization_version: int}
     */
    public static function normalizeV1(string $url): array
    {
        $canonical = self::canonicalizeV1($url);

        return [
            'source_url' => $canonical['canonical_url'],
            'source_url_hash' => $canonical['url_id'],
            'source_domain' => $canonical['source_domain'],
            'source_path' => $canonical['source_path'],
            'source_url_canonicalization_version' => self::CANONICALIZATION_VERSION,
        ];
    }

    /**
     * Canonicalize a URL without DNS or network access.
     *
     * @return array{canonical_url: string, url_id: string, canonicalization_version: int, source_domain: string, source_path: string, crawl_eligible: bool, crawl_rejection: string|null}
     */
    public static function canonicalizeV1(string $url): array
    {
        self::validateRawInput($url);

        if (preg_match('/\A([A-Za-z][A-Za-z0-9+.-]*):/', $url, $schemeMatch) !== 1) {
            self::fail('absolute_url_required');
        }

        $scheme = strtolower($schemeMatch[1]);

        if (! in_array($scheme, ['http', 'https'], true)) {
            self::fail('scheme_not_allowed');
        }

        $schemePrefixLength = strlen($schemeMatch[0]);
        if (substr($url, $schemePrefixLength, 2) !== '//') {
            self::fail('absolute_url_required');
        }

        $afterScheme = substr($url, $schemePrefixLength + 2);
        $authorityLength = strcspn($afterScheme, '/?#');
        $authority = substr($afterScheme, 0, $authorityLength);
        $suffix = substr($afterScheme, $authorityLength);

        if ($authority === '') {
            self::fail('absolute_url_required');
        }

        if (str_contains($authority, '@')) {
            self::fail('userinfo_forbidden');
        }

        [$host, $hostForUrl, $port, $isIpLiteral] = self::parseAuthority($authority);
        $sourcePath = self::canonicalizePathAndQuery($suffix);

        $portForUrl = '';
        if ($port !== null && ! self::isDefaultPort($scheme, $port)) {
            $portForUrl = ':'.$port;
        }

        $canonicalUrl = $scheme.'://'.$hostForUrl.$portForUrl.$sourcePath;

        if (strlen($canonicalUrl) > self::MAX_URL_BYTES) {
            self::fail('url_too_long');
        }

        $crawlRejection = self::crawlRejection($scheme, $host, $port, $isIpLiteral);

        return [
            'canonical_url' => $canonicalUrl,
            'url_id' => self::urlIdV1($canonicalUrl),
            'canonicalization_version' => self::CANONICALIZATION_VERSION,
            'source_domain' => $host,
            'source_path' => $sourcePath,
            'crawl_eligible' => $crawlRejection === null,
            'crawl_rejection' => $crawlRejection,
        ];
    }

    public static function urlIdV1(string $canonicalUrl): string
    {
        return hash('sha256', self::URL_ID_NAMESPACE.$canonicalUrl);
    }

    private static function validateRawInput(string $url): void
    {
        if (strlen($url) > self::MAX_URL_BYTES) {
            self::fail('url_too_long');
        }

        if (preg_match('//u', $url) !== 1) {
            self::fail('invalid_utf8');
        }

        if (preg_match('/(?:\A[\s\p{Z}]|[\s\p{Z}]\z|\p{Cc})/u', $url) === 1) {
            self::fail('whitespace_or_control');
        }

        if (str_contains($url, '\\')) {
            self::fail('backslash_forbidden');
        }

        if (preg_match('/%(?![A-Fa-f0-9]{2})/', $url) === 1) {
            self::fail('malformed_escape');
        }

        if (self::containsEncodedControl($url)) {
            self::fail('encoded_control');
        }
    }

    private static function containsEncodedControl(string $url): bool
    {
        preg_match_all('/(?:%[A-Fa-f0-9]{2})+/', $url, $encodedRuns);

        foreach ($encodedRuns[0] as $encodedRun) {
            $bytes = '';

            for ($index = 0; $index < strlen($encodedRun); $index += 3) {
                $byte = hexdec(substr($encodedRun, $index + 1, 2));

                if ($byte < 0x20 || $byte === 0x7F) {
                    return true;
                }

                $bytes .= chr($byte);
            }

            if (preg_match('//u', $bytes) === 1 && preg_match('/\p{Cc}/u', $bytes) === 1) {
                return true;
            }
        }

        return false;
    }

    /**
     * @return array{string, string, int|null, bool}
     */
    private static function parseAuthority(string $authority): array
    {
        $port = null;
        $rawPort = null;
        $isIpLiteral = false;

        if (str_starts_with($authority, '[')) {
            $closingBracket = strpos($authority, ']');

            if ($closingBracket === false) {
                self::fail('invalid_host');
            }

            $host = substr($authority, 1, $closingBracket - 1);
            $afterHost = substr($authority, $closingBracket + 1);

            if ($afterHost !== '') {
                if (! str_starts_with($afterHost, ':')) {
                    self::fail('invalid_host');
                }

                $rawPort = substr($afterHost, 1);
            }

            if ($host === '' || filter_var($host, FILTER_VALIDATE_IP, FILTER_FLAG_IPV6) === false) {
                self::fail('invalid_host');
            }

            $host = strtolower($host);
            $isIpLiteral = true;
            $port = $rawPort === null ? null : self::parsePort($rawPort);

            return [$host, '['.$host.']', $port, $isIpLiteral];
        }

        if (str_contains($authority, '[') || str_contains($authority, ']') || substr_count($authority, ':') > 1) {
            self::fail('invalid_host');
        }

        if (str_contains($authority, ':')) {
            [$host, $rawPort] = explode(':', $authority, 2);
        } else {
            $host = $authority;
        }

        if ($host === '') {
            self::fail('invalid_host');
        }

        if (preg_match('/[^\x00-\x7F]/', $host) === 1) {
            self::fail('non_ascii_host_v1');
        }

        $host = strtolower($host);

        if (filter_var($host, FILTER_VALIDATE_IP, FILTER_FLAG_IPV4) !== false) {
            $isIpLiteral = true;
        } else {
            if (preg_match('/\A[0-9]+(?:\.[0-9]+){3}\z/', $host) === 1) {
                self::fail('invalid_host');
            }

            self::validateHostname($host);
        }

        $port = $rawPort === null ? null : self::parsePort($rawPort);

        return [$host, $host, $port, $isIpLiteral];
    }

    private static function parsePort(string $rawPort): int
    {
        if (preg_match('/\A[0-9]+\z/', $rawPort) !== 1) {
            self::fail('invalid_port');
        }

        $significantDigits = ltrim($rawPort, '0');
        if ($significantDigits === '' || strlen($significantDigits) > 5) {
            self::fail('invalid_port');
        }

        $port = (int) $rawPort;
        if ($port < 1 || $port > 65535) {
            self::fail('invalid_port');
        }

        return $port;
    }

    private static function validateHostname(string $host): void
    {
        if ($host === '' || strlen($host) > 253 || str_ends_with($host, '.')) {
            self::fail('invalid_host');
        }

        foreach (explode('.', $host) as $label) {
            if (strlen($label) < 1
                || strlen($label) > 63
                || preg_match('/\A[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\z/', $label) !== 1) {
                self::fail('invalid_host');
            }

            if (str_starts_with($label, 'xn--')) {
                self::validateIdnaALabel($label);
            }
        }
    }

    private static function validateIdnaALabel(string $label): void
    {
        if (! function_exists('idn_to_utf8') || ! function_exists('idn_to_ascii')) {
            self::fail('invalid_host');
        }

        $unicodeInfo = [];
        $unicodeLabel = idn_to_utf8(
            $label,
            IDNA_NONTRANSITIONAL_TO_UNICODE
                | IDNA_USE_STD3_RULES
                | IDNA_CHECK_BIDI
                | IDNA_CHECK_CONTEXTJ,
            INTL_IDNA_VARIANT_UTS46,
            $unicodeInfo
        );

        if ($unicodeLabel === false || ($unicodeInfo['errors'] ?? 1) !== 0) {
            self::fail('invalid_host');
        }

        $asciiInfo = [];
        $roundTrip = idn_to_ascii(
            $unicodeLabel,
            IDNA_NONTRANSITIONAL_TO_ASCII
                | IDNA_USE_STD3_RULES
                | IDNA_CHECK_BIDI
                | IDNA_CHECK_CONTEXTJ,
            INTL_IDNA_VARIANT_UTS46,
            $asciiInfo
        );

        if ($roundTrip === false
            || ($asciiInfo['errors'] ?? 1) !== 0
            || strtolower($roundTrip) !== $label) {
            self::fail('invalid_host');
        }
    }

    private static function canonicalizePathAndQuery(string $suffix): string
    {
        $fragmentPosition = strpos($suffix, '#');
        if ($fragmentPosition !== false) {
            $suffix = substr($suffix, 0, $fragmentPosition);
        }

        $queryPosition = strpos($suffix, '?');
        $query = null;

        if ($queryPosition === false) {
            $path = $suffix;
        } else {
            $path = substr($suffix, 0, $queryPosition);
            $query = substr($suffix, $queryPosition + 1);
        }

        if ($path === '') {
            $path = '/';
        }

        if (! str_starts_with($path, '/')) {
            self::fail('absolute_url_required');
        }

        $path = self::encodeComponent($path, self::PATH_SAFE_CHARACTERS);
        $sourcePath = $path;

        if ($query !== null) {
            $query = self::encodeComponent($query, self::QUERY_SAFE_CHARACTERS);
            $sourcePath .= '?'.$query;
        }

        return $sourcePath;
    }

    private static function encodeComponent(string $value, string $safeCharacters): string
    {
        $encoded = '';
        $length = strlen($value);

        for ($index = 0; $index < $length; $index++) {
            $character = $value[$index];

            if ($character === '%') {
                $encoded .= '%'.strtoupper(substr($value, $index + 1, 2));
                $index += 2;

                continue;
            }

            $byte = ord($character);
            if ($byte >= 128 || ! str_contains($safeCharacters, $character)) {
                $encoded .= sprintf('%%%02X', $byte);

                continue;
            }

            $encoded .= $character;
        }

        return $encoded;
    }

    private static function isDefaultPort(string $scheme, int $port): bool
    {
        return ($scheme === 'http' && $port === 80)
            || ($scheme === 'https' && $port === 443);
    }

    private static function crawlRejection(string $scheme, string $host, ?int $port, bool $isIpLiteral): ?string
    {
        if ($isIpLiteral) {
            return 'ip_literal_forbidden';
        }

        if (self::isLocalHostname($host)) {
            return 'local_name_forbidden';
        }

        if ($port !== null && ! self::isDefaultPort($scheme, $port)) {
            return 'non_default_crawl_port';
        }

        return null;
    }

    private static function isLocalHostname(string $host): bool
    {
        if (! str_contains($host, '.')) {
            return true;
        }

        foreach (['localhost', 'local', 'localdomain', 'internal', 'intranet', 'home', 'home.arpa', 'lan', 'test', 'invalid'] as $localName) {
            if ($host === $localName || str_ends_with($host, '.'.$localName)) {
                return true;
            }
        }

        return false;
    }

    /**
     * @return never
     */
    private static function fail(string $reason): void
    {
        throw new SourceUrlNormalizationException($reason);
    }
}
