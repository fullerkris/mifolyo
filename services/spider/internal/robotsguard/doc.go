// Package robotsguard fetches, caches, and enforces robots.txt policies for
// canonical crawl-policy decisions. All network access is delegated to
// securefetch, so robots requests share the crawler's policy, redirect, DNS,
// and request-gate controls.
package robotsguard
