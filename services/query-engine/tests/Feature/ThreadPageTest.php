<?php

namespace Tests\Feature;

use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Tests\TestCase;

class ThreadPageTest extends TestCase
{
    private array $post = ['id' => 12, 'title' => 'Useful source', 'body' => 'A complete discussion.', 'source_url' => 'https://example.test/a?b=1&c=two', 'comment_count' => 2];

    private array $comments = [['id' => 4, 'body' => '<script>bad()</script>', 'author' => ['username' => 'reader'], 'depth' => 0, 'children' => [
        ['id' => 5, 'body' => 'Nested perspective', 'depth' => 1, 'children' => []],
    ]]];

    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    private function fakeForum(array $overrides = []): void
    {
        Http::fake($overrides + [
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/posts/12' => Http::response(['data' => $this->post]),
            'forum.test/api/posts/12/comments' => Http::response(['data' => $this->comments]),
        ]);
    }

    private function signIn(): void
    {
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]]);
    }

    public function test_guest_reads_full_thread_and_nested_replies_with_safe_content_and_source_links(): void
    {
        $this->fakeForum();
        $response = $this->get('/threads/12');
        $response->assertOk()->assertSee('A complete discussion.')->assertSee('Nested perspective')
            ->assertSee('id="comment-5"', false)->assertSee('href="https://example.test/a?b=1&amp;c=two"', false)
            ->assertSee('&lt;script&gt;bad()&lt;/script&gt;', false)->assertDontSee('<script>bad()', false)
            ->assertSee('Sign in to reply')->assertDontSee('name="body"', false);
        $this->assertStringContainsString('no-store', $response->headers->get('Cache-Control'));
    }

    public function test_reply_target_survives_reauthentication_and_can_be_explicitly_changed_to_the_thread(): void
    {
        $this->fakeForum();
        $this->signIn();
        $this->withSession(['forum_drafts' => [12 => ['body' => 'Saved perspective', 'parent_comment_id' => 4]]]);
        $this->get('/threads/12')->assertOk()->assertSee('Reply to reader')->assertSee('Saved perspective')
            ->assertSee('name="parent_comment_id" value="4"', false);
        $this->get('/threads/12?reply_to=0')->assertOk()->assertSee('name="parent_comment_id" value=""', false)->assertSee('Saved perspective');
        $this->get('/threads/12?reply_to=999')->assertOk()->assertSee('selected comment is unavailable')->assertDontSee('name="body"', false);
    }

    public function test_comments_outage_preserves_the_thread_and_closed_threads_have_no_composer(): void
    {
        $this->post['is_locked'] = true;
        $this->fakeForum(['forum.test/api/posts/12/comments' => Http::failedConnection()]);
        $this->signIn();
        $this->get('/threads/12')->assertOk()->assertSee('A complete discussion.')->assertSee('Replies are temporarily unavailable')
            ->assertSee('closed to new replies')->assertDontSee('name="body"', false);
    }

    public function test_missing_private_and_unavailable_threads_have_actionable_errors(): void
    {
        Http::fake(['forum.test/api/posts/12' => Http::sequence()
            ->push(['message' => 'internal details'], 403)->push(['message' => 'internal details'], 404)->push(['message' => 'internal details'], 503)]);
        foreach ([403 => 'This conversation is private', 404 => 'Conversation unavailable', 503 => 'Community temporarily unavailable'] as $status => $heading) {
            $this->get('/threads/12')->assertStatus($status)->assertSee($heading)->assertDontSee('internal details');
        }
    }

    public function test_reply_is_posted_with_the_server_token_and_redirects_to_the_saved_comment(): void
    {
        $this->fakeForum(['forum.test/api/posts/12/comments' => Http::response(['data' => ['id' => 9]], 201)]);
        $this->signIn();
        $this->post('/threads/12/comments', ['body' => 'My perspective', 'parent_comment_id' => 4])
            ->assertRedirect('/threads/12#comment-9')->assertSessionMissing('forum_drafts.12');
        Http::assertSent(fn ($request) => $request->method() === 'POST' && $request['parent_comment_id'] === 4 && $request['body'] === 'My perspective'
            && $request->hasHeader('Authorization', 'Bearer server-token'));
    }

    public function test_expired_write_keeps_reply_draft_for_sign_in(): void
    {
        $this->fakeForum(['forum.test/api/posts/12/comments' => Http::response(['message' => 'Token expired.'], 401)]);
        $this->signIn();
        $this->post('/threads/12/comments', ['body' => 'Saved perspective', 'parent_comment_id' => 4])
            ->assertRedirect('/sign-in?return_to=%2Fthreads%2F12')->assertSessionMissing('forum')
            ->assertSessionHas('forum_drafts.12', ['body' => 'Saved perspective', 'parent_comment_id' => 4]);
    }

    public function test_uncertain_writes_keep_drafts_and_do_not_retry_automatically(): void
    {
        $this->fakeForum(['forum.test/api/posts/12/comments' => Http::failedConnection()]);
        $this->signIn();
        $this->post('/threads/12/comments', ['body' => 'Do not lose this'])
            ->assertRedirect('/threads/12')->assertSessionHasErrors('body')->assertSessionHas('forum_drafts.12.body', 'Do not lose this');
        Http::assertSentCount(2); // Account lookup and exactly one write.
    }

    public function test_unsafe_source_links_are_not_rendered(): void
    {
        $this->post['source_url'] = 'javascript:alert(1)';
        $this->fakeForum();
        $this->get('/threads/12')->assertOk()->assertDontSee('href="javascript:', false);
    }

    public function test_author_levels_are_visible_without_inventing_levels_for_missing_or_removed_authors(): void
    {
        $this->post['author'] = ['username' => '<script>author</script>', 'level' => 4];
        $this->comments[0]['author']['level'] = 2;
        $this->comments[0]['children'][0]['author'] = ['username' => 'Hidden author', 'level' => 8];
        $this->comments[0]['children'][0]['is_removed'] = true;
        $this->fakeForum();
        $this->get('/threads/12')->assertOk()->assertSee('Level 4')->assertSee('Level 2')->assertSee('Community member')
            ->assertDontSee('Level 8')->assertDontSee('Hidden author')->assertDontSee('<script>author</script>', false);
    }
}
