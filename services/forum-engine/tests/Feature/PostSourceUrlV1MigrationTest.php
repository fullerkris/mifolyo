<?php

namespace Tests\Feature;

use App\Models\Community;
use App\Models\User;
use App\Support\SourceUrlNormalizer;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;
use Tests\TestCase;

class PostSourceUrlV1MigrationTest extends TestCase
{
    use RefreshDatabase;

    public function test_migration_round_trip_restores_legacy_hashes_without_narrowing_long_urls(): void
    {
        $migration = $this->migration();
        $migration->down();

        $owner = User::factory()->create();
        $community = Community::query()->create([
            'owner_user_id' => $owner->id,
            'name' => 'Legacy Links',
            'slug' => 'legacy-links',
            'is_private' => false,
        ]);

        $validUrl = 'HTTPS://Example.COM:443/straße?x=a+b#fragment';
        $legacySourceUrl = 'https://example.com/straße?x=a+b';
        $legacySourceHash = hash('sha256', $legacySourceUrl);
        $invalidUrl = ' https://example.com/invalid';
        $textUrl = 'https://example.com/text-post';
        $maximumLengthUrl = 'https://example.com/'.str_repeat(
            'a',
            SourceUrlNormalizer::MAX_URL_BYTES - strlen('https://example.com/')
        );
        $maximumLengthSource = SourceUrlNormalizer::normalizeV1($maximumLengthUrl);

        $validId = $this->insertLegacyPost($community->id, 'valid-link', 'link', $validUrl, [
            'source_url' => $legacySourceUrl,
            'source_url_hash' => $legacySourceHash,
            'source_domain' => 'example.com',
            'source_path' => '/straße?x=a+b',
        ]);
        $invalidId = $this->insertLegacyPost($community->id, 'invalid-link', 'link', $invalidUrl, [
            'source_url' => 'https://example.com/invalid',
            'source_url_hash' => hash('sha256', 'https://example.com/invalid'),
            'source_domain' => 'example.com',
            'source_path' => '/invalid',
        ]);
        $textId = $this->insertLegacyPost($community->id, 'text-post', 'text', $textUrl);
        $maximumLengthId = $this->insertLegacyPost(
            $community->id,
            'maximum-length-link',
            'link',
            $maximumLengthUrl,
            [
                'source_url' => $maximumLengthSource['source_url'],
                'source_url_hash' => hash('sha256', $maximumLengthSource['source_url']),
                'source_domain' => $maximumLengthSource['source_domain'],
                'source_path' => $maximumLengthSource['source_path'],
            ]
        );

        $migration->up();

        $this->assertTrue(Schema::hasColumn('posts', 'source_url_canonicalization_version'));
        $this->assertTrue(Schema::hasTable('post_source_url_v1_backups'));
        $this->assertDatabaseCount('post_source_url_v1_backups', 4);

        $expected = SourceUrlNormalizer::normalizeV1($validUrl);
        $this->assertDatabaseHas('posts', array_merge([
            'id' => $validId,
            'url' => $expected['source_url'],
        ], $expected));

        $this->assertDatabaseHas('posts', [
            'id' => $invalidId,
            'url' => $invalidUrl,
            'source_url' => 'https://example.com/invalid',
            'source_url_hash' => hash('sha256', 'https://example.com/invalid'),
            'source_url_canonicalization_version' => null,
            'source_domain' => 'example.com',
            'source_path' => '/invalid',
        ]);
        $this->assertDatabaseHas('posts', [
            'id' => $textId,
            'url' => $textUrl,
            'source_url_canonicalization_version' => null,
        ]);
        $this->assertDatabaseHas('posts', array_merge([
            'id' => $maximumLengthId,
            'url' => $maximumLengthUrl,
        ], $maximumLengthSource));

        $createdAfterUpSource = SourceUrlNormalizer::normalizeV1('https://example.com/created-after-up?');
        $createdAfterUpLegacyUrl = 'https://example.com/created-after-up';
        $createdAfterUpId = $this->insertLegacyPost(
            $community->id,
            'created-after-up',
            'link',
            $createdAfterUpSource['source_url'],
            $createdAfterUpSource
        );

        $migration->down();

        try {
            $this->assertFalse(Schema::hasColumn('posts', 'source_url_canonicalization_version'));
            $this->assertFalse(Schema::hasTable('post_source_url_v1_backups'));
            $this->assertDatabaseHas('posts', [
                'id' => $validId,
                'url' => $validUrl,
                'source_url' => $legacySourceUrl,
                'source_url_hash' => $legacySourceHash,
                'source_domain' => 'example.com',
                'source_path' => '/straße?x=a+b',
            ]);
            $this->assertSame(
                $validId,
                DB::table('posts')
                    ->where('source_url_hash', $legacySourceHash)
                    ->where('source_url', $legacySourceUrl)
                    ->value('id')
            );
            $this->assertDatabaseHas('posts', [
                'id' => $maximumLengthId,
                'url' => $maximumLengthUrl,
                'source_url' => $maximumLengthUrl,
                'source_url_hash' => hash('sha256', $maximumLengthUrl),
            ]);
            $this->assertDatabaseHas('posts', [
                'id' => $createdAfterUpId,
                'url' => $createdAfterUpLegacyUrl,
                'source_url' => $createdAfterUpLegacyUrl,
                'source_url_hash' => hash('sha256', $createdAfterUpLegacyUrl),
            ]);
            $this->assertSame(
                $maximumLengthUrl,
                DB::table('posts')->where('id', $maximumLengthId)->value('url')
            );
        } finally {
            $migration->up();
        }

        $this->assertTrue(Schema::hasColumn('posts', 'source_url_canonicalization_version'));
        $this->assertTrue(Schema::hasTable('post_source_url_v1_backups'));
        $this->assertDatabaseHas('posts', array_merge([
            'id' => $maximumLengthId,
            'url' => $maximumLengthUrl,
        ], $maximumLengthSource));
    }

    /**
     * @param  array<string, mixed>  $overrides
     */
    private function insertLegacyPost(
        int $communityId,
        string $slug,
        string $contentType,
        string $url,
        array $overrides = []
    ): int {
        return DB::table('posts')->insertGetId(array_merge([
            'community_id' => $communityId,
            'author_user_id' => null,
            'title' => str_replace('-', ' ', $slug),
            'slug' => $slug,
            'body' => null,
            'url' => $url,
            'source_url' => null,
            'source_url_hash' => null,
            'source_domain' => null,
            'source_path' => null,
            'content_type' => $contentType,
            'published_at' => now(),
            'created_at' => now(),
            'updated_at' => now(),
        ], $overrides));
    }

    private function migration(): Migration
    {
        return require database_path('migrations/2026_08_02_000400_upgrade_post_source_urls_to_v1.php');
    }
}
