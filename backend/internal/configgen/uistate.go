package configgen

import (
	"os"

	"github.com/bzmshang/mihomo-ui/internal/store"
	"gopkg.in/yaml.v3"
)

// UIState is the panel overlay applied last when installing config.
//
// TunEnable is always written: the panel owns it outright, which is what makes
// TUN off the product default even when the subscription turns it on. TunStack
// and AllowLan are opt-in instead — empty and unset mean the panel never touched
// them, so override.yaml and the subscription keep deciding.
type UIState struct {
	Mode      string
	LogLevel  string
	TunEnable bool
	TunStack  string
	AllowLan  bool
	// AllowLanSet distinguishes "the panel switched allow-lan off" from "the
	// panel never touched it": without it the overlay's zero value would pin
	// allow-lan to false and override.yaml could never set it.
	AllowLanSet bool
}

// DefaultUIState is the product default (TUN off even if base has tun.enable).
func DefaultUIState() UIState {
	return UIState{
		Mode:      "rule",
		LogLevel:  "info",
		TunEnable: false,
	}
}

// DefaultUIStateFromOverride reads mode / log-level from override.yaml only.
func DefaultUIStateFromOverride(overridePath string) UIState {
	st := DefaultUIState()
	if overridePath == "" {
		return st
	}
	raw, err := os.ReadFile(overridePath)
	if err != nil {
		return st
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil || doc == nil {
		return st
	}
	if m, ok := doc["mode"].(string); ok && m != "" {
		st.Mode = m
	}
	if lv, ok := doc["log-level"].(string); ok && lv != "" {
		st.LogLevel = lv
	}
	return st
}

// UIStateFromPrefs maps store prefs into install overlay.
func UIStateFromPrefs(p store.UIPrefs) UIState {
	st := UIState{
		Mode:      p.Mode,
		LogLevel:  p.LogLevel,
		TunEnable: p.TunEnable,
		TunStack:  p.TunStack,
	}
	if p.AllowLan != nil {
		st.AllowLan = *p.AllowLan
		st.AllowLanSet = true
	}
	return st
}

// Overlay applies panel switches on top of a merged config map.
func (st UIState) Overlay(root map[string]any) {
	if root == nil {
		return
	}
	if st.Mode != "" {
		root["mode"] = st.Mode
	}
	if st.LogLevel != "" {
		root["log-level"] = st.LogLevel
	}
	tun := asMap(root["tun"])
	if tun == nil {
		tun = map[string]any{}
	} else {
		tun = cloneMap(tun)
	}
	tun["enable"] = st.TunEnable
	// Empty means the panel never picked a stack, so the one override.yaml or the
	// subscription provided stands. The panel only offers the picker once a stack
	// is present, which is also what makes a switch (rather than a select) honest
	// in a deployment where override.yaml sets every other tun field.
	if st.TunStack != "" {
		tun["stack"] = st.TunStack
	}
	root["tun"] = tun
	// Same rule as the stack: unset means the panel never touched it, so
	// override.yaml and the subscription keep deciding allow-lan.
	if st.AllowLanSet {
		root["allow-lan"] = st.AllowLan
	}
}
