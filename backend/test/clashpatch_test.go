package test

import (
	"testing"
)

// The panel applies its switches itself: zashboard PATCHes /configs through the
// same-origin proxy and stores nothing. Everything below is about the half the
// kernel cannot do — making that switch outlive the next config apply.
//
// Each test installs the boot config first, because that is the state the panel
// serves from: main.go always writes config.yaml before it starts listening, and
// the mirror patches the file it wrote.

// installBoot writes the config.yaml a freshly started panel would be serving.
func installBoot(t *testing.T, e *env) {
	t.Helper()
	if _, err := e.Svc.ApplyActive(); err != nil {
		t.Fatalf("boot install: %v", err)
	}
}

// The TUN toggle is the panel's substitute for a tun.enable row that the old
// overlay used to own, so a patch that reaches the kernel has to reach
// settings.yaml too. Without the second half the next apply rebuilds config.yaml
// from override.yaml and the subscription and the switch silently reverts.
func TestProxiedConfigPatchIsPersistedAsAPrefAndMirrored(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	code, _ := e.json("PATCH", "/clash/configs", tok, `{"tun":{"enable":true}}`)
	if code != 204 {
		t.Fatalf("PATCH /clash/configs = %d, want the kernel's 204", code)
	}
	if p := e.Kernel.lastPatch(); p == nil {
		t.Fatal("the patch never reached the kernel")
	}
	if !e.Store.Prefs().TunEnable {
		t.Fatal("tun-enable was not persisted, so the next apply would revert it")
	}
	tun, _ := e.readConfigYAML()["tun"].(map[string]any)
	if tun == nil || tun["enable"] != true {
		t.Fatalf("config.yaml tun = %v, want the mirrored enable", tun)
	}

	// The mirror has to survive an apply, which is the whole point: this install
	// merges override.yaml (tun.enable absent) and the subscription over the pref.
	cfg := e.addConfig("c", sampleConfig("G1"))
	if _, _, err := e.Svc.Activate(cfg.ID, false); err != nil {
		t.Fatal(err)
	}
	tun, _ = e.readConfigYAML()["tun"].(map[string]any)
	if tun == nil || tun["enable"] != true {
		t.Fatalf("after apply tun = %v, want the persisted enable to win", tun)
	}
}

// zashboard sends whatever it thinks changed, including a stack it switched away
// from. A stack pinned to the empty string would take the stack out of whatever
// override.yaml set, so only a real one is recorded.
func TestProxiedPatchRecordsOnlyRealTunStack(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	if code, _ := e.json("PATCH", "/clash/configs", tok, `{"tun":{"enable":true,"stack":"gvisor"}}`); code != 204 {
		t.Fatalf("patch with stack = %d", code)
	}
	if got := e.Store.Prefs().TunStack; got != "gvisor" {
		t.Fatalf("tun-stack pref = %q, want gvisor", got)
	}

	if code, _ := e.json("PATCH", "/clash/configs", tok, `{"tun":{"enable":true,"stack":""}}`); code != 204 {
		t.Fatalf("patch with empty stack = %d", code)
	}
	if got := e.Store.Prefs().TunStack; got != "gvisor" {
		t.Fatalf("tun-stack pref = %q, want the previous stack to stand", got)
	}
	tun, _ := e.readConfigYAML()["tun"].(map[string]any)
	if tun == nil || tun["stack"] != "gvisor" {
		t.Fatalf("config.yaml tun = %v, want the stack left alone", tun)
	}
}

// A switch the kernel refused did not happen, and recording it would have the
// panel claim a setting the kernel is not running.
func TestRejectedProxiedPatchIsNotPersisted(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()
	e.Kernel.rejectPatches()

	code, _ := e.json("PATCH", "/clash/configs", tok, `{"tun":{"enable":true}}`)
	if code == 204 {
		t.Fatalf("a rejected patch still read as accepted: %d", code)
	}
	if e.Store.Prefs().TunEnable {
		t.Fatal("a rejected patch was persisted anyway")
	}
	tun, _ := e.readConfigYAML()["tun"].(map[string]any)
	if tun != nil && tun["enable"] == true {
		t.Fatalf("config.yaml tun = %v, want nothing mirrored", tun)
	}
}

// allow-lan is the other switch the panel owns, and it has one wrinkle: the
// pref has to stay unset until the panel actually flips it, or the overlay would
// pin allow-lan to its zero value and override.yaml could never set it.
func TestProxiedPatchCarriesAllowLanWithoutPinningIt(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	if e.Store.Prefs().AllowLan != nil {
		t.Fatal("allow-lan was set before the panel touched it")
	}
	if _, ok := e.readConfigYAML()["allow-lan"]; !ok {
		// override.yaml ships allow-lan: false, so the untouched value has to come
		// from there rather than from the pref.
		t.Fatalf("allow-lan missing from config.yaml before any panel switch: %v", e.readConfigYAML())
	}

	if code, _ := e.json("PATCH", "/clash/configs", tok, `{"allow-lan":true}`); code != 204 {
		t.Fatalf("patch = %d", code)
	}
	if p := e.Store.Prefs().AllowLan; p == nil || !*p {
		t.Fatalf("allow-lan pref = %v, want true", p)
	}
	if got := e.readConfigYAML()["allow-lan"]; got != true {
		t.Fatalf("config.yaml allow-lan = %v, want the mirrored true", got)
	}
}

// mode and log-level are panel switches with the same problem as TUN, so the
// same proxy path has to carry them.
func TestProxiedPatchCarriesModeAndLogLevel(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	if code, _ := e.json("PATCH", "/clash/configs", tok, `{"mode":"global","log-level":"debug"}`); code != 204 {
		t.Fatalf("patch = %d", code)
	}
	if got := e.Store.Prefs().Mode; got != "global" {
		t.Fatalf("mode pref = %q", got)
	}
	if got := e.Store.Prefs().LogLevel; got != "debug" {
		t.Fatalf("log-level pref = %q", got)
	}
	doc := e.readConfigYAML()
	if doc["mode"] != "global" || doc["log-level"] != "debug" {
		t.Fatalf("config.yaml = %v / %v", doc["mode"], doc["log-level"])
	}
}

// The body has to reach the kernel whole: reading it to record the patch must
// not leave the proxied request with nothing to send.
func TestProxiedPatchBodyStillReachesTheKernel(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	if code, _ := e.json("PATCH", "/clash/configs", tok, `{"tun":{"enable":true,"stack":"system"}}`); code != 204 {
		t.Fatalf("patch = %d", code)
	}
	cfg := e.Kernel.configKeys()
	tun, _ := cfg["tun"].(map[string]any)
	if tun == nil || tun["enable"] != true || tun["stack"] != "system" {
		t.Fatalf("kernel saw tun = %v, want the request body intact", tun)
	}
}

// Only PATCH /configs is a switch. Everything else the panel does through this
// proxy — proxy selection, delay tests, connection closes — must pass through
// untouched.
func TestProxiedNonConfigRequestsAreUntouched(t *testing.T) {
	e := newEnv(t)
	installBoot(t, e)
	tok := e.login()

	code, body := e.json("GET", "/clash/echo", tok, "")
	if code != 200 {
		t.Fatalf("GET /clash/echo = %d", code)
	}
	if body["path"] != "/echo" {
		t.Fatalf("kernel saw path %v", body["path"])
	}
	if e.Store.Prefs().Mode != "rule" {
		t.Fatalf("an unrelated request changed a pref: %q", e.Store.Prefs().Mode)
	}
}
