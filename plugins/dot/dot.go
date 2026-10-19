// Package dot provides the optional Dot background task operator page.
package dot

import "github.com/yetone/magpie/internal/pluginrt"

func init() {
	pluginrt.Register(pluginrt.Manifest{
		ID: "dot", API: "^1", Requires: []string{"window"},
		Name: "Dot background tasks", Sub: "DotControl",
		Parts:  []pluginrt.Part{{Slot: "nav.page", Key: "dot"}},
		Client: []string{"i18n.js", "dot.css", "client.js"},
	})
}
