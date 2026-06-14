<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::table('posts', function (Blueprint $table) {
            $table->string('source_url', 2048)->nullable()->after('url');
            $table->string('source_url_hash', 64)->nullable()->after('source_url');
            $table->string('source_domain')->nullable()->after('source_url_hash');
            $table->string('source_path', 2048)->nullable()->after('source_domain');

            $table->index('source_url_hash');
            $table->index('source_domain');
        });

        DB::table('posts')
            ->whereNotNull('url')
            ->orderBy('id')
            ->select(['id', 'url'])
            ->get()
            ->each(function (object $post): void {
                $normalized = $this->normalizeSourceUrl((string) $post->url);

                if ($normalized === null) {
                    return;
                }

                DB::table('posts')
                    ->where('id', $post->id)
                    ->update($normalized);
            });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::table('posts', function (Blueprint $table) {
            $table->dropIndex(['source_url_hash']);
            $table->dropIndex(['source_domain']);
            $table->dropColumn(['source_url', 'source_url_hash', 'source_domain', 'source_path']);
        });
    }

    /**
     * @return array{source_url: string, source_url_hash: string, source_domain: string, source_path: string}|null
     */
    private function normalizeSourceUrl(string $url): ?array
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
