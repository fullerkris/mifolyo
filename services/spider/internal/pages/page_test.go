package pages

import (
	"testing"
)

func TestRenderedPageHashRoundTripPreservesBothArtifacts(t *testing.T) {
	page := CreateRenderedPage(
		"https://render.example.org/app",
		"<html><main id='root'></main></html>",
		"<html><main id='root'>rendered</main></html>",
		"text/html",
		200,
		"inline-fixture",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	hash, err := HashPage(page)
	if err != nil {
		t.Fatal(err)
	}
	stringsHash := make(map[string]string, len(hash))
	for key, value := range hash {
		if text, ok := value.(string); ok {
			stringsHash[key] = text
		} else {
			stringsHash[key] = "200"
		}
	}
	restored, err := DehashPage(stringsHash)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.Rendered || restored.OriginalHTML != page.OriginalHTML || restored.HTML != page.HTML ||
		restored.RenderPolicyRule != page.RenderPolicyRule || restored.RenderPolicySHA256 != page.RenderPolicySHA256 {
		t.Fatalf("restored page = %#v", restored)
	}
}

func TestStaticPageRejectsRenderedProvenance(t *testing.T) {
	page := CreatePage("https://example.org/", "<html></html>", "text/html", 200)
	page.OriginalHTML = "<html></html>"
	if _, err := HashPage(page); err == nil {
		t.Fatal("static page with rendered provenance was accepted")
	}
}
