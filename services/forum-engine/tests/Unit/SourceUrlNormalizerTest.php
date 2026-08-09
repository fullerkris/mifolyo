<?php

namespace Tests\Unit;

use App\Support\SourceUrlNormalizationException;
use App\Support\SourceUrlNormalizer;
use JsonException;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use RuntimeException;

class SourceUrlNormalizerTest extends TestCase
{
    /**
     * @param  array{name: string, input: string, canonical_url: string, url_id: string, crawl_eligible: bool, crawl_rejection: string|null}  $fixture
     */
    #[DataProvider('validUrlFixtures')]
    public function test_it_matches_shared_valid_v1_fixtures(array $fixture): void
    {
        $canonical = SourceUrlNormalizer::canonicalizeV1($fixture['input']);
        $source = SourceUrlNormalizer::normalize($fixture['input']);

        $this->assertSame($fixture['canonical_url'], $canonical['canonical_url']);
        $this->assertSame($fixture['url_id'], $canonical['url_id']);
        $this->assertSame(SourceUrlNormalizer::CANONICALIZATION_VERSION, $canonical['canonicalization_version']);
        $this->assertSame($fixture['crawl_eligible'], $canonical['crawl_eligible']);
        $this->assertSame($fixture['crawl_rejection'], $canonical['crawl_rejection']);
        $this->assertSame($fixture['canonical_url'], $source['source_url']);
        $this->assertSame($fixture['url_id'], $source['source_url_hash']);
        $this->assertSame(
            SourceUrlNormalizer::CANONICALIZATION_VERSION,
            $source['source_url_canonicalization_version']
        );

        $this->assertSame($canonical, SourceUrlNormalizer::canonicalizeV1($canonical['canonical_url']));
    }

    /**
     * @param  array{name: string, input: string, error: string}  $fixture
     */
    #[DataProvider('invalidUrlFixtures')]
    public function test_it_returns_stable_errors_for_shared_invalid_v1_fixtures(array $fixture): void
    {
        try {
            SourceUrlNormalizer::normalizeV1($fixture['input']);
        } catch (SourceUrlNormalizationException $exception) {
            $this->assertSame($fixture['error'], $exception->reason);
            $this->assertSame($fixture['error'], $exception->getMessage());

            return;
        }

        $this->fail('Expected URL normalization to fail with '.$fixture['error'].'.');
    }

    public function test_v1_identity_constants_match_the_shared_contract(): void
    {
        $contract = self::contract();

        $this->assertSame($contract['version'], SourceUrlNormalizer::CANONICALIZATION_VERSION);
        $this->assertSame($contract['id_namespace'], SourceUrlNormalizer::URL_ID_NAMESPACE);
        $this->assertSame($contract['max_url_bytes'], SourceUrlNormalizer::MAX_URL_BYTES);

        $canonicalUrl = 'https://example.com/identity';
        $this->assertSame(
            hash('sha256', $contract['id_namespace'].$canonicalUrl),
            SourceUrlNormalizer::urlIdV1($canonicalUrl)
        );
    }

    public function test_it_rejects_a_structurally_invalid_idna_a_label(): void
    {
        try {
            SourceUrlNormalizer::normalizeV1('https://xn--abc.example/');
        } catch (SourceUrlNormalizationException $exception) {
            $this->assertSame('invalid_host', $exception->errorCode());

            return;
        }

        $this->fail('Expected malformed IDNA A-label validation to fail.');
    }

    /**
     * @return iterable<string, array{array<string, mixed>}>
     */
    public static function validUrlFixtures(): iterable
    {
        foreach (self::contract()['valid'] as $fixture) {
            yield $fixture['name'] => [$fixture];
        }
    }

    /**
     * @return iterable<string, array{array<string, mixed>}>
     */
    public static function invalidUrlFixtures(): iterable
    {
        foreach (self::contract()['invalid'] as $fixture) {
            yield $fixture['name'] => [$fixture];
        }
    }

    /**
     * @return array<string, mixed>
     *
     * @throws JsonException
     */
    private static function contract(): array
    {
        $path = dirname(__DIR__, 4).'/contracts/url-canonicalization/v1.json';
        $contents = file_get_contents($path);

        if ($contents === false) {
            throw new RuntimeException('Unable to read the shared URL canonicalization contract.');
        }

        return json_decode($contents, true, flags: JSON_THROW_ON_ERROR);
    }
}
