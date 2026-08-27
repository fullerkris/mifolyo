package crawler

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawlpolicy"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderclient"
	"github.com/IonelPopJara/search-engine/services/spider/internal/renderpolicy"
)

type pageRenderResult struct {
	HTML         string
	RuleID       string
	PolicySHA256 string
}

type pageRenderBinding struct {
	Depth    int
	Decision crawlpolicy.Decision
	Gate     *crawlRequestGate
}

func (crawcfg *CrawlerConfig) renderIfRequired(
	ctx context.Context,
	effectiveURL string,
	sourceHTML string,
	binding *pageRenderBinding,
) (pageRenderResult, error) {
	if crawcfg.RenderPolicy == nil {
		return pageRenderResult{HTML: sourceHTML}, nil
	}
	rule, err := crawcfg.RenderPolicy.Match(effectiveURL)
	if errors.Is(err, renderpolicy.ErrNoMatchingRule) {
		return pageRenderResult{HTML: sourceHTML}, nil
	}
	if err != nil {
		return pageRenderResult{}, fmt.Errorf("match render policy: %w", err)
	}
	if crawcfg.Renderer == nil {
		return pageRenderResult{}, fmt.Errorf("render rule %q matched but no renderer is configured", rule.ID)
	}
	if !utf8.ValidString(sourceHTML) {
		return pageRenderResult{}, fmt.Errorf("render rule %q requires valid UTF-8 source HTML", rule.ID)
	}
	if len(crawcfg.RenderPolicySHA256) != 64 ||
		strings.ToLower(crawcfg.RenderPolicySHA256) != crawcfg.RenderPolicySHA256 {
		return pageRenderResult{}, fmt.Errorf("render rule %q has no valid policy digest", rule.ID)
	}
	if _, err := hex.DecodeString(crawcfg.RenderPolicySHA256); err != nil {
		return pageRenderResult{}, fmt.Errorf("render rule %q has no valid policy digest", rule.ID)
	}
	var broker renderclient.ResourceBroker
	if rule.Mode == renderpolicy.ModeBrokered {
		if binding == nil {
			return pageRenderResult{}, fmt.Errorf("render rule %q has no fetched-page broker binding", rule.ID)
		}
		broker, err = crawcfg.newPageResourceBroker(
			effectiveURL,
			rule,
			binding.Depth,
			binding.Decision,
			binding.Gate,
		)
		if err != nil {
			return pageRenderResult{}, fmt.Errorf("render rule %q: initialize resource broker: %w", rule.ID, err)
		}
	}
	crawcfg.renderMu.Lock()
	defer crawcfg.renderMu.Unlock()
	result, err := crawcfg.Renderer.Render(ctx, renderclient.Job{
		EffectiveURL: effectiveURL,
		HTML:         sourceHTML,
		Rule:         rule,
		Broker:       broker,
	})
	if err != nil {
		return pageRenderResult{}, fmt.Errorf("render rule %q: %w", rule.ID, err)
	}
	return pageRenderResult{
		HTML:         result.HTML,
		RuleID:       rule.ID,
		PolicySHA256: crawcfg.RenderPolicySHA256,
	}, nil
}
