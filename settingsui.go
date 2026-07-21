package stremio

import (
	"context"
	"encoding/json"

	v1 "github.com/mosaic-media/mosaic-sdk/contracts/platform/v1"
	sdui "github.com/mosaic-media/mosaic-sdui/sdui"
)

// SettingsUI renders the module's own settings screen as SDUI (RoleSettingsUI,
// ADR 0038): add an addon by manifest URL, view the installed addons with a way
// to remove them, toggle the bundled Cinemeta default, and browse a catalog of
// installable addons (the addon_catalog resource) to add without a URL. Every
// mutating control is an Invoke of the Platform's configureModule command with
// the complete new settings document, so the Platform stays the one that
// persists them. The screen is returned as serialised UINode JSON — the SDK
// stays SDUI-agnostic.
func (c *Capability) SettingsUI(ctx context.Context, req v1.SettingsUIRequest) (v1.SettingsUIResponse, error) {
	addons, disableDefaults, err := addonsFrom(req.Settings)
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}

	body := []sdui.Node{
		addAddonSection(addons, disableDefaults),
		installedSection(addons, disableDefaults),
		c.browseSection(ctx, req.Settings, addons, disableDefaults),
	}
	screen := sdui.Screen(sdui.Prop("title", "Stremio addons"), sdui.Child(body...))

	ui, err := json.Marshal(screen)
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}
	return v1.SettingsUIResponse{UI: ui}, nil
}

// configureInput builds the configureModule invoke input for a given addon list
// and default flag — the complete settings document the Platform will persist.
func configureInput(addons []string, disableDefaults bool) map[string]any {
	settings := map[string]any{"addons": addons}
	if disableDefaults {
		settings["disableDefaultAddons"] = true
	}
	return map[string]any{"moduleId": CapabilityID, "settings": settings}
}

// addAddonSection is the add-by-URL form: a SubmitField whose action carries the
// existing addons plus the "$value" placeholder the runtime fills with the typed
// manifest URL (ADR 0038).
func addAddonSection(addons []string, disableDefaults bool) sdui.Node {
	withNew := append(append([]string{}, addons...), "$value")
	field := sdui.Component("SubmitField",
		sdui.Prop("placeholder", "Paste an addon manifest URL…"),
		sdui.Prop("submitLabel", "Add"),
		sdui.Act(sdui.Invoke("configureModule", configureInput(withNew, disableDefaults))),
	)
	return sdui.Section("Add an addon", sdui.Child(field))
}

// installedSection lists the bundled default and every configured addon, each
// with the control that changes it: enable/disable the default, remove an addon.
func installedSection(addons []string, disableDefaults bool) sdui.Node {
	rows := make([]sdui.Node, 0, len(addons)+1)

	// The bundled Cinemeta default, with a toggle.
	if disableDefaults {
		rows = append(rows, addonRow("Cinemeta — bundled default (disabled)",
			sdui.Button("Enable", "secondary", sdui.Invoke("configureModule", configureInput(addons, false)))))
	} else {
		rows = append(rows, addonRow("Cinemeta — bundled default",
			sdui.Button("Disable", "ghost", sdui.Invoke("configureModule", configureInput(addons, true)))))
	}

	for _, a := range addons {
		rows = append(rows, addonRow(a,
			sdui.Button("Remove", "danger", sdui.Invoke("configureModule", configureInput(without(addons, a), disableDefaults)))))
	}

	return sdui.Section("Installed addons",
		sdui.Child(sdui.Stack("vertical", 3, sdui.Child(rows...))))
}

// browseSection lists installable addons from the addon_catalog resource, each
// with an Install control. Best-effort: with no addon-catalog source configured
// it shows an empty state rather than vanishing, so the feature is discoverable.
func (c *Capability) browseSection(ctx context.Context, settings []byte, addons []string, disableDefaults bool) sdui.Node {
	client, err := c.clientFrom(settings)
	if err != nil {
		return sdui.Section("Browse addons", sdui.Child(sdui.EmptyState("collections", err.Error())))
	}
	entries, err := client.AddonCatalog(ctx)
	if err != nil || len(entries) == 0 {
		return sdui.Section("Browse addons",
			sdui.Child(sdui.EmptyState("collections", "No addon catalog available — configure an addon that provides one to browse installable addons here")))
	}

	rows := make([]sdui.Node, 0, len(entries))
	for _, e := range entries {
		name := e.Manifest.Name
		if name == "" {
			name = e.TransportURL
		}
		installed := contains(addons, e.TransportURL)
		if installed {
			rows = append(rows, addonRow(name+" — installed", sdui.Badge("Installed", sdui.ToneSuccess)))
			continue
		}
		withNew := append(append([]string{}, addons...), e.TransportURL)
		rows = append(rows, addonRow(name,
			sdui.Button("Install", "primary", sdui.Invoke("configureModule", configureInput(withNew, disableDefaults)))))
	}
	return sdui.Section("Browse addons",
		sdui.Child(sdui.Stack("vertical", 3, sdui.Child(rows...))))
}

// addonRow is a labelled row with a trailing control.
func addonRow(label string, control sdui.Node) sdui.Node {
	return sdui.Component("Box",
		sdui.Prop("style", map[string]any{
			"direction": "row", "align": "center", "justify": "between",
			"gap": 4, "px": 4, "py": 3, "radius": "md", "bg": "surface-raised", "border": true,
		}),
		sdui.Child(
			sdui.Component("Text", sdui.Prop("text", label), sdui.Prop("style", map[string]any{"variant": "sm"})),
			control,
		),
	)
}

// without returns addons with the first occurrence of target removed.
func without(addons []string, target string) []string {
	out := make([]string, 0, len(addons))
	removed := false
	for _, a := range addons {
		if !removed && a == target {
			removed = true
			continue
		}
		out = append(out, a)
	}
	return out
}
