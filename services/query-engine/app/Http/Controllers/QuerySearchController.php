<?php

namespace App\Http\Controllers;

use Illuminate\Http\Request;
use Illuminate\Support\Facades\DB;

class QuerySearchController extends Controller
{
    public function get_page_connections(Request $request)
    {
        $validated = $request->validate([
            'url' => ['required', 'url:http,https', 'max:2048'],
        ]);
        $url = $validated['url'];

        // Fetch page outlinks
        $outlinksData = DB::connection('mongodb')
            ->table('outlinks')
            ->where('id', $url)
            ->first();

        // if (!$outlinksData) {
        //     return response()->json([
        //         'status' => 'error',
        //         'message' => 'URL not found in outlinks database'
        //     ], 404);
        // }

        $outlinks = $outlinksData->links ?? [];

        // Fetch metadata for each outlink
        $enrichedOutlinks = [];
        if (count($outlinks) > 0) {
            $metadataCollection = DB::connection('mongodb')
                ->table('metadata')
                ->whereIn('_id', $outlinks)
                ->get();

            $metadataMap = [];
            foreach ($metadataCollection as $metadata) {
                $metadataMap[$metadata->id] = $metadata;
            }

            foreach ($outlinks as $link) {
                if (isset($metadataMap[$link])) {
                    $enrichedOutlinks[] = [
                        'url' => $link,
                        'title' => $metadataMap[$link]->title ?? 'Page Not Indexed',
                    ];
                } else {
                    $enrichedOutlinks[] = [
                        'url' => $link,
                        'title' => 'Page Not Indexed',
                    ];
                }
            }
        }

        // Fetch page backlinks
        $backlinksData = DB::connection('mongodb')
            ->table('backlinks')
            ->where('id', $url)
            ->first();

        // if (!$backlinksData) {
        //     return response()->json([
        //         'status' => 'error',
        //         'message' => 'URL not found in backlinks database'
        //     ], 404);
        // }

        $backlinks = $backlinksData->links ?? [];

        // Fetch metadata for each backlink
        $enrichedBacklinks = [];
        if (count($backlinks) > 0) {
            $metadataCollection = DB::connection('mongodb')
                ->table('metadata')
                ->whereIn('_id', $backlinks)
                ->get();

            $metadataMap = [];
            foreach ($metadataCollection as $metadata) {
                $metadataMap[$metadata->id] = $metadata;
            }

            foreach ($backlinks as $link) {
                if (isset($metadataMap[$link])) {
                    $enrichedBacklinks[] = [
                        'url' => $link,
                        'title' => $metadataMap[$link]->title ?? 'Page Not Indexed',
                    ];
                } else {
                    $enrichedBacklinks[] = [
                        'url' => $link,
                        'title' => 'Page Not Indexed',
                    ];
                }
            }
        }

        // Get url metadata
        $urlMetadata = DB::connection('mongodb')
            ->table('metadata')
            ->where('_id', $url)
            ->first();

        return view('page-connections', [
            'url' => $url,
            'title' => $urlMetadata->title ?? 'Page Not Indexed',
            'outlinks' => $enrichedOutlinks,
            'backlinks' => $enrichedBacklinks,
        ]);
    }

    public function getTopImages($query, $page = 1, $perPage = 5)
    {
        // Split the query string
        $query = str_replace('+', ' ', $query);
        $words = explode(' ', mb_strtolower($query, 'UTF-8'));

        // Use a count aggregation to get total results more efficiently
        $countPipeline = [
            ['$match' => ['word' => ['$in' => $words]]],
            ['$set' => ['_associationId' => ['$ifNull' => ['$association_id', '$url']]]],
            ['$group' => ['_id' => '$_associationId']],
            ['$count' => 'total'],
        ];

        $countResult = DB::connection('mongodb')
            ->table('word_images')
            ->raw(fn ($collection) => $collection->aggregate(
                $countPipeline,
                ['maxTimeMS' => 2000],
            )->toArray());

        $totalResults = isset($countResult[0]) ? $countResult[0]['total'] : 0;

        // Aggregation
        $paginationPipeline = [
            ['$match' => ['word' => ['$in' => $words]]],
            ['$set' => ['_associationId' => ['$ifNull' => ['$association_id', '$url']]]],
            [
                '$group' => [
                    '_id' => '$_associationId',
                    'cumWeight' => ['$sum' => '$weight'],
                    'matchedWords' => ['$addToSet' => '$word'],
                    'matchCount' => ['$sum' => 1],
                ],
            ],
            ['$sort' => ['matchCount' => -1, 'cumWeight' => -1]],
            ['$skip' => ($page - 1) * $perPage],
            ['$limit' => $perPage],
        ];

        // Get paginated results
        $paginatedResults = DB::connection('mongodb')
            ->table('word_images')
            ->raw(function ($collection) use ($paginationPipeline) {
                // Use a cursor to iterate through the results
                $cursor = $collection->aggregate($paginationPipeline, [
                    'cursor' => ['batchSize' => 20],
                    'maxTimeMS' => 2000,
                ]);
                $results = [];
                foreach ($cursor as $document) {
                    $results[] = $document;
                }

                return $results;
            });

        // Populate the metadata for each URL in the paginated results
        $associationIds = array_map(fn ($result) => $result['_id'], $paginatedResults);

        // Fetch image data
        $imagesData = DB::connection('mongodb')->table('images')
            ->whereIn('_id', $associationIds)
            ->get();

        // First, reindex the metadata by _id for fast lookup
        $imageDataByUrl = [];
        foreach ($imagesData as $data) {
            $imageDataByUrl[$data->id] = $data;
        }

        // Get all the page urls
        $pageUrls = [];
        foreach ($imageDataByUrl as $result) {
            $pageUrls[] = $result->page_url ?? '';
        }

        // Fetch all pages metadata
        $pageMetadataList = DB::connection('mongodb')->table('metadata')
            ->whereIn('_id', $pageUrls)
            ->get();

        // Reindex page metadata by _id
        $pageMetadataByUrl = [];
        foreach ($pageMetadataList as $meta) {
            $pageMetadataByUrl[$meta->id] = $meta;
        }

        // Merge image data into each paginated result
        foreach ($paginatedResults as &$result) {
            $imageData = $imageDataByUrl[$result['_id']] ?? null;
            // Mongo groups and joins by the page-image association. Preserve
            // the existing public shape by exposing the actual source URL as
            // _id (legacy source-keyed rows naturally fall back to their id).
            $result['_id'] = $imageData->source_url ?? $result['_id'];
            $result['alt'] = $imageData->alt ?? '';
            $result['filname'] = $imageData->filename ?? '';
            $result['page_url'] = $imageData->page_url ?? '';
            $pageMetadata = $pageMetadataByUrl[$result['page_url']] ?? null;
            $result['page_title'] = $pageMetadata->title ?? '';
            // Shorten the summary text to 300 characters
            $result['page_text'] = '';
            $length = 100;
            if (isset($pageMetadata->summary_text)) {
                $result['page_text'] = mb_strlen($pageMetadata->summary_text, 'UTF-8') > $length
                    ? mb_substr($pageMetadata->summary_text, 0, $length, 'UTF-8').'...'
                    : $pageMetadata->summary_text;
            }
        }

        return [$paginatedResults, $totalResults];
    }

    public function stats()
    {
        $results = DB::connection('mongodb')->table('metadata')->count();

        return response()->json([
            'status' => 'up',
            'pages' => $results,
        ]);
    }

    public function search(Request $request)
    {
        $validated = $request->validate([
            'q' => [
                'bail',
                'nullable',
                'string',
                'max:500',
                static function (string $attribute, mixed $value, \Closure $fail): void {
                    if (! mb_check_encoding($value, 'UTF-8')) {
                        $fail('The query must be valid UTF-8.');
                    }
                },
            ],
            'page' => ['sometimes', 'integer', 'min:1', 'max:100'],
        ]);
        $hasSuggestions = $request->attributes->get('hasSuggestions', false);
        $originalQuery = $validated['q'] ?? null;
        $processedQuery = $request->attributes->get('processedQuery');
        $query = $processedQuery !== null && $processedQuery !== '' ? $processedQuery : $originalQuery;
        if ($query === null || $query === '') {
            $query = '';
            if ($request->wantsJson()) {
                return response()->json([
                    'query' => $query,
                    'results' => [],
                    'meta' => [
                        'total' => 0,
                        'page' => 0,
                        'source' => 'query-engine',
                    ],
                ]);
            }

            return view('search-results', [
                'query' => $query,
                'results' => [],
                'total' => 0,
                'topImages' => [],
                'suggestions' => $hasSuggestions,
                'originalQuery' => $originalQuery,
                'page' => 0,
            ]);
        }

        // Split the query string
        $query = str_replace('+', ' ', $query);
        $words = preg_split('/\s+/u', mb_strtolower(trim($query), 'UTF-8'), -1, PREG_SPLIT_NO_EMPTY);
        abort_if($words === false, 422, 'The query must be valid UTF-8.');
        abort_if(count($words) > 20, 422, 'Too many search terms.');

        // Set the number of results per page
        $perPage = 20;
        $page = (int) ($validated['page'] ?? 1);

        // Use a count aggregation to get total results more efficiently
        $countPipeline = [
            ['$match' => ['word' => ['$in' => $words]]],
            ['$group' => ['_id' => '$url']],
            ['$count' => 'total'],
        ];

        $countResult = DB::connection('mongodb')
            ->table('words')
            ->raw(fn ($collection) => $collection->aggregate(
                $countPipeline,
                ['maxTimeMS' => 2000],
            )->toArray());

        $totalResults = isset($countResult[0]) ? $countResult[0]['total'] : 0;

        $paginationPipeline = $this->buildSearchPaginationPipeline($words, (int) $page, $perPage);

        // Get paginated results
        $paginatedResults = DB::connection('mongodb')
            ->table('words')
            ->raw(function ($collection) use ($paginationPipeline) {
                // Use a cursor to iterate through the results
                $cursor = $collection->aggregate($paginationPipeline, [
                    'allowDiskUse' => true,
                    'cursor' => ['batchSize' => 20],
                    'maxTimeMS' => 2000,
                ]);
                $results = [];
                foreach ($cursor as $document) {
                    $results[] = $document;
                }

                return $results;
            });

        // Populate the metadata for each URL in the paginated results
        $urls = array_map(fn ($result) => $result['_id'], $paginatedResults);

        $metadata = DB::connection('mongodb')->table('metadata')
            ->whereIn('_id', $urls)
            ->get();

        // First, reindex the metadata by _id for fast lookup
        $metadataByUrl = [];
        foreach ($metadata as $meta) {
            $metadataByUrl[$meta->id] = $meta;
        }

        // Now, merge metadata into each paginated result
        foreach ($paginatedResults as &$result) {
            $resultMetadata = $metadataByUrl[$result['_id']] ?? null;
            $result['description'] = $resultMetadata->description ?? '';
            $result['last_crawled'] = $resultMetadata->last_crawled ?? '';
            $result['summary_text'] = $resultMetadata->summary_text ?? '';
            $result['title'] = $resultMetadata->title ?? '';
        }

        if ($request->wantsJson()) {
            return response()->json([
                'query' => $query,
                'results' => $paginatedResults,
                'meta' => [
                    'total' => $totalResults,
                    'page' => (int) $page,
                    'per_page' => $perPage,
                    'source' => 'query-engine',
                ],
            ]);
        }

        // Get top 5 images if it's the first page
        $topImages = [];
        if ($page == 1) {
            [$topImages, $unused] = $this->getTopImages($query, $page, 5);
        }

        // Return view for SSR
        return view('search-results', [
            'query' => $query,
            'results' => $paginatedResults,
            'total' => $totalResults,
            'topImages' => $topImages,
            'suggestions' => $hasSuggestions,
            'originalQuery' => $originalQuery,
            'page' => $page,
        ]);
    }

    private function buildSearchPaginationPipeline(array $words, int $page, int $perPage): array
    {
        return [
            ['$match' => ['word' => ['$in' => $words]]],
            [
                '$group' => [
                    '_id' => '$url',
                    'cumWeight' => ['$sum' => '$weight'],
                    'matchedWords' => ['$addToSet' => '$word'],
                    'matchCount' => ['$sum' => 1],
                ],
            ],
            [
                '$lookup' => [
                    'from' => 'pagerank',
                    'localField' => '_id',
                    'foreignField' => '_id',
                    'as' => '_pagerankDocument',
                ],
            ],
            [
                '$set' => [
                    '_rawTfidf' => [
                        '$convert' => [
                            'input' => '$cumWeight',
                            'to' => 'double',
                            'onError' => 0.0,
                            'onNull' => 0.0,
                        ],
                    ],
                    '_rawPageRank' => [
                        '$convert' => [
                            'input' => ['$arrayElemAt' => ['$_pagerankDocument.rank', 0]],
                            'to' => 'double',
                            'onError' => 0.0,
                            'onNull' => 0.0,
                        ],
                    ],
                ],
            ],
            [
                '$set' => [
                    '_tfidfForScore' => [
                        '$cond' => [
                            [
                                '$and' => [
                                    ['$isNumber' => '$_rawTfidf'],
                                    ['$gte' => ['$_rawTfidf', 0.0]],
                                    ['$lte' => ['$_rawTfidf', PHP_FLOAT_MAX]],
                                ],
                            ],
                            '$_rawTfidf',
                            0.0,
                        ],
                    ],
                    'pagerank' => [
                        '$cond' => [
                            [
                                '$and' => [
                                    ['$isNumber' => '$_rawPageRank'],
                                    ['$gte' => ['$_rawPageRank', 0.0]],
                                    ['$lte' => ['$_rawPageRank', 1.0]],
                                ],
                            ],
                            '$_rawPageRank',
                            0.0,
                        ],
                    ],
                ],
            ],
            ['$set' => ['cumWeight' => '$_tfidfForScore']],
            [
                '$setWindowFields' => [
                    'output' => [
                        '_maxTfidf' => [
                            '$max' => '$_tfidfForScore',
                            'window' => ['documents' => ['unbounded', 'unbounded']],
                        ],
                        '_maxPageRank' => [
                            '$max' => '$pagerank',
                            'window' => ['documents' => ['unbounded', 'unbounded']],
                        ],
                    ],
                ],
            ],
            [
                '$set' => [
                    '_normalizedTfidf' => [
                        '$cond' => [
                            ['$gt' => [['$ifNull' => ['$_maxTfidf', 0.0]], 0.0]],
                            ['$divide' => ['$_tfidfForScore', '$_maxTfidf']],
                            0.0,
                        ],
                    ],
                    '_normalizedPageRank' => [
                        '$cond' => [
                            ['$gt' => [['$ifNull' => ['$_maxPageRank', 0.0]], 0.0]],
                            ['$divide' => ['$pagerank', '$_maxPageRank']],
                            0.0,
                        ],
                    ],
                ],
            ],
            [
                '$set' => [
                    'combinedScore' => [
                        '$add' => [
                            ['$multiply' => [0.6, '$_normalizedTfidf']],
                            ['$multiply' => [0.4, '$_normalizedPageRank']],
                        ],
                    ],
                ],
            ],
            ['$sort' => ['matchCount' => -1, 'combinedScore' => -1, '_id' => 1]],
            ['$skip' => ($page - 1) * $perPage],
            ['$limit' => $perPage],
            [
                '$unset' => [
                    '_pagerankDocument',
                    '_rawTfidf',
                    '_rawPageRank',
                    '_tfidfForScore',
                    '_maxTfidf',
                    '_maxPageRank',
                    '_normalizedTfidf',
                    '_normalizedPageRank',
                ],
            ],
        ];
    }

    public function search_images(Request $request)
    {
        $validated = $request->validate([
            'q' => [
                'bail',
                'nullable',
                'string',
                'max:500',
                static function (string $attribute, mixed $value, \Closure $fail): void {
                    if (! mb_check_encoding($value, 'UTF-8')) {
                        $fail('The query must be valid UTF-8.');
                    }
                },
            ],
            'page' => ['sometimes', 'integer', 'min:1', 'max:100'],
            'suggestions' => ['sometimes', 'boolean'],
        ]);
        $suggestions = (bool) ($validated['suggestions'] ?? false);
        $originalQuery = $validated['q'] ?? null;
        // $query = $request->input('processed_query');
        $query = $originalQuery;
        if ($query === null || $query === '') {
            $query = '';

            return view('search-image-results', [
                'query' => $query,
                'results' => [],
                'total' => 0,
                'topImages' => [],
                'suggestions' => $suggestions,
                'originalQuery' => $originalQuery,
            ]);
        }

        // Split the query string
        $query = str_replace('+', ' ', $query);
        $words = preg_split('/\s+/u', mb_strtolower(trim($query), 'UTF-8'), -1, PREG_SPLIT_NO_EMPTY);
        abort_if($words === false, 422, 'The query must be valid UTF-8.');
        abort_if(count($words) > 20, 422, 'Too many search terms.');

        // Set the number of results per page
        $perPage = 20;
        $page = (int) ($validated['page'] ?? 1);

        [$paginatedResults, $totalResults] = $this->getTopImages($query, $page, $perPage);

        return view('search-image-results', [
            'query' => $query,
            'results' => $paginatedResults,
            'total' => $totalResults,
            'topImages' => [],
            'suggestions' => $suggestions,
            'originalQuery' => $originalQuery,
        ]);
    }

    public function get_top_ranked_page(Request $request)
    {
        // Get the top ranked page from pagerank
        $results = DB::connection('mongodb')->table('pagerank')
            ->orderBy('rank', 'desc')
            ->limit(1)
            ->get();
        if ($results->count() <= 0) {
            return null;
        }

        // Fetch the page metadata
        $page_metadata = DB::connection('mongodb')->table('metadata')
            ->where('_id', $results[0]->id)
            ->first();
        if (! $page_metadata) {
            return null;
        }

        // Return the page metadata as an array
        return [
            'title' => $page_metadata->title,
            'url' => $page_metadata->id,
            'description' => $page_metadata->description,
            'last_crawled' => $page_metadata->last_crawled,
            'summary_text' => $page_metadata->summary_text,
        ];
    }

    // Make a function to get a random page from the metadata collection
    public function get_random_page(Request $request)
    {
        $results = DB::connection('mongodb')
            ->table('metadata')
            ->raw(function ($collection) {
                return $collection->aggregate([
                    ['$sample' => ['size' => 1]],
                ]);
            });

        $document = $results->toArray();

        if (empty($document)) {
            return null;
        }

        $doc = $document[0];

        // Return the page metadata as an array
        return [
            'title' => $doc['title'],
            'url' => $doc['_id'],
            'description' => $doc['description'],
            'last_crawled' => $doc['last_crawled'],
            'summary_text' => $doc['summary_text'],
        ];
    }

    public function get_dictionary(Request $request)
    {
        $validated = $request->validate([
            'limit' => ['sometimes', 'integer', 'min:1', 'max:500'],
            'page' => ['sometimes', 'integer', 'min:1'],
        ]);
        $limit = (int) ($validated['limit'] ?? 100);
        $page = (int) ($validated['page'] ?? 1);
        $total = DB::connection('mongodb')->table('dictionary')->count();
        $results = DB::connection('mongodb')
            ->table('dictionary')
            ->orderBy('_id')
            ->skip(($page - 1) * $limit)
            ->limit($limit)
            ->pluck('id'); // ONLY get the word strings

        return response()->json([
            'status' => 'up',
            'dictionary' => $results,
            'meta' => [
                'total' => $total,
                'page' => $page,
                'per_page' => $limit,
                'last_page' => (int) ceil($total / $limit),
            ],
        ]);
    }
}
