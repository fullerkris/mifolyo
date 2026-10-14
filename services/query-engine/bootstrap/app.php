<?php

use Illuminate\Database\QueryException;
use Illuminate\Foundation\Application;
use Illuminate\Foundation\Configuration\Exceptions;
use Illuminate\Foundation\Configuration\Middleware;
use Illuminate\Http\Request;
use Illuminate\Validation\ValidationException;
use MongoDB\Driver\Exception\Exception as MongoException;
use Symfony\Component\HttpKernel\Exception\HttpExceptionInterface;

return Application::configure(basePath: dirname(__DIR__))
    ->withRouting(
        web: __DIR__.'/../routes/web.php',
        api: __DIR__.'/../routes/api.php',
        commands: __DIR__.'/../routes/console.php',
        health: '/up',
    )
    ->withMiddleware(function (Middleware $middleware) {
        $middleware->web(append: [\App\Http\Middleware\ResolveForumAccount::class]);
    })
    ->withExceptions(function (Exceptions $exceptions) {
        $exceptions->render(function (Throwable $exception, Request $request) {
            if ($exception instanceof HttpExceptionInterface && $request->is('threads/*', 'community/*', 'sign-in', 'register', 'sign-out')) {
                $status = $exception->getStatusCode();
                if (in_array($status, [401, 403, 404, 419, 429, 503], true)) {
                    [$heading, $message] = match ($status) {
                        401 => ['Please sign in again', 'Your session has expired. Sign in to continue the conversation.'],
                        403 => ['This conversation is private', 'Only members of this community can read this conversation.'],
                        404 => ['Conversation unavailable', 'This thread may have been removed or the link may be incorrect.'],
                        419 => ['This page has expired', 'Reload the page before submitting your form again.'],
                        429 => ['A moment to catch up', 'Please wait a minute before trying again.'],
                        default => ['Community temporarily unavailable', 'We could not reach the community service. Please try again shortly.'],
                    };

                    return ($request->expectsJson() ? response()->json(['message' => $message], $status)
                        : response()->view('community-error', compact('heading', 'message', 'status'), $status))
                        ->withHeaders($exception->getHeaders())->header('Cache-Control', 'private, no-store');
                }
            }
            if ($request->expectsJson() || ! $request->is('search', 'search/exact', 'api/search', 'api/search_force')) {
                return null;
            }

            $status = match (true) {
                $exception instanceof ValidationException => 422,
                $exception instanceof MongoException, $exception instanceof QueryException => 503,
                $exception instanceof HttpExceptionInterface => $exception->getStatusCode(),
                default => null,
            };
            if (! in_array($status, [422, 429, 503], true)) {
                return null;
            }

            [$heading, $message] = match ($status) {
                422 => ['Let’s adjust that search', 'Use up to 20 search terms (500 characters) and a page between 1 and 100. Edit your search above and try again.'],
                429 => ['A moment to catch up', 'You’ve made several searches in a short time. Please wait a minute, then try again.'],
                default => ['Search is temporarily unavailable', 'We couldn’t reach the search index. Your query is still here — please try again shortly.'],
            };
            $query = $request->query('q', '');
            $query = is_string($query) && mb_check_encoding($query, 'UTF-8') ? mb_substr($query, 0, 500) : '';
            $page = filter_var($request->query('page', 1), FILTER_VALIDATE_INT);
            $route = $request->is('search/exact', 'api/search_force') ? 'search.exact' : 'search';
            $retryUrl = $status === 422 ? null : route($route, ['q' => $query, 'page' => max(1, min(100, $page ?: 1))]);

            return response()->view('search-error', compact('status', 'heading', 'message', 'query', 'retryUrl'), $status,
                $exception instanceof HttpExceptionInterface ? $exception->getHeaders() : []);
        });
    })->create();
