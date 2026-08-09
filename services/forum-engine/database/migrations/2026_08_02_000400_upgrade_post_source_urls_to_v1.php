<?php

use App\Support\SourceUrlNormalizationException;
use App\Support\SourceUrlNormalizer;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    private const string LOOKUP_INDEX = 'posts_source_url_v1_lookup_index';

    private const string BACKUP_TABLE = 'post_source_url_v1_backups';

    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create(self::BACKUP_TABLE, function (Blueprint $table): void {
            $table->unsignedBigInteger('post_id')->primary();
            $table->text('url')->nullable();
            $table->text('source_url')->nullable();
            $table->string('source_url_hash', 64)->nullable();
            $table->string('source_domain')->nullable();
            $table->text('source_path')->nullable();
        });

        DB::table('posts')
            ->select(['id', 'url', 'source_url', 'source_url_hash', 'source_domain', 'source_path'])
            ->chunkById(500, function ($posts): void {
                $backups = [];

                foreach ($posts as $post) {
                    $backups[] = [
                        'post_id' => $post->id,
                        'url' => $post->url,
                        'source_url' => $post->source_url,
                        'source_url_hash' => $post->source_url_hash,
                        'source_domain' => $post->source_domain,
                        'source_path' => $post->source_path,
                    ];
                }

                if ($backups !== []) {
                    DB::table(self::BACKUP_TABLE)->insert($backups);
                }
            });

        Schema::table('posts', function (Blueprint $table): void {
            $table->string('url', SourceUrlNormalizer::MAX_URL_BYTES)->nullable()->change();
            $table->unsignedTinyInteger('source_url_canonicalization_version')
                ->nullable()
                ->after('source_url_hash');
            $table->index(
                ['source_url_hash', 'source_url_canonicalization_version'],
                self::LOOKUP_INDEX
            );
        });

        DB::table('posts')
            ->where('content_type', 'link')
            ->whereNotNull('url')
            ->select(['id', 'url'])
            ->chunkById(500, function ($posts): void {
                foreach ($posts as $post) {
                    try {
                        $source = SourceUrlNormalizer::normalizeV1((string) $post->url);
                    } catch (SourceUrlNormalizationException) {
                        continue;
                    }

                    DB::table('posts')
                        ->where('id', $post->id)
                        ->update(array_merge($source, [
                            'url' => $source['source_url'],
                        ]));
                }
            });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        // Rows without a backup were created while V1 was active. Convert
        // their canonical source fields to the legacy identity contract;
        // backed-up rows are restored exactly in the following pass.
        DB::table('posts')
            ->whereNotNull('source_url')
            ->select(['id', 'source_url'])
            ->chunkById(500, function ($posts): void {
                foreach ($posts as $post) {
                    $legacySource = $this->normalizeLegacySourceUrl((string) $post->source_url);

                    if ($legacySource === null) {
                        $legacySource = [
                            'source_url_hash' => hash('sha256', (string) $post->source_url),
                        ];
                    } else {
                        $legacySource['url'] = $legacySource['source_url'];
                    }

                    DB::table('posts')
                        ->where('id', $post->id)
                        ->update($legacySource);
                }
            });

        DB::table(self::BACKUP_TABLE)
            ->select(['post_id', 'url', 'source_url', 'source_url_hash', 'source_domain', 'source_path'])
            ->chunkById(500, function ($backups): void {
                foreach ($backups as $backup) {
                    DB::table('posts')
                        ->where('id', $backup->post_id)
                        ->update([
                            'url' => $backup->url,
                            'source_url' => $backup->source_url,
                            'source_url_hash' => $backup->source_url_hash,
                            'source_domain' => $backup->source_domain,
                            'source_path' => $backup->source_path,
                        ]);
                }
            }, 'post_id');

        Schema::table('posts', function (Blueprint $table): void {
            $table->dropIndex(self::LOOKUP_INDEX);
            $table->dropColumn('source_url_canonicalization_version');
        });

        Schema::drop(self::BACKUP_TABLE);
    }

    /**
     * @return array{source_url: string, source_url_hash: string, source_domain: string, source_path: string}|null
     */
    private function normalizeLegacySourceUrl(string $url): ?array
    {
        $parts = parse_url(trim($url));

        if (! is_array($parts) || empty($parts['scheme']) || empty($parts['host'])) {
            return null;
        }

        $scheme = strtolower($parts['scheme']);
        if (! in_array($scheme, ['http', 'https'], true)) {
            return null;
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
};
