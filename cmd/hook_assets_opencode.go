package cmd

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed hook_assets/opencode/cymbal-opencode.js
//go:embed hook_assets/opencode/cymbal-opencode.v2.js
var opencodeHookAssets embed.FS

func renderOpenCodePlugin(marker, version string) string {
	return renderOpenCodeAsset(marker, version, "hook_assets/opencode/cymbal-opencode.js")
}

func renderOpenCodeV2Plugin(marker, version string) string {
	return renderOpenCodeAsset(marker, version, "hook_assets/opencode/"+opencodeManagedPluginV2File)
}

func renderOpenCodeAsset(marker, version, name string) string {
	body, err := opencodeHookAssets.ReadFile(name)
	if err != nil {
		panic(fmt.Errorf("read embedded OpenCode plugin asset: %w", err))
	}
	content := string(body)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return fmt.Sprintf("// %s managed by cymbal\n// cymbal-version: %s\n%s", marker, version, content)
}
