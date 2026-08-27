package pages

import (
// "fmt"
)

type Image struct {
	NormalizedPageURL   string
	NormalizedSourceURL string
	Alt                 string
}

// func (img Image) String() string {
//     altText := img.Alt
//     if altText == "" {
//         altText = "(none)"
//     }
//
//     return fmt.Sprintf(
//         "Image Found:\n"+
//         "  Page URL:  %s\n"+
//         "  Source URL: %s\n"+
//         "  Alt Text:  %s\n",
//         img.NormalizedPageURL, img.NormalizedSourceURL, altText,
//     )
// }
