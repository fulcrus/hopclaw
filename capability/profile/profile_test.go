package profile

import "testing"

func TestDefaultRegistryResolvesDesktopProfileAndAction(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry()
	resolution := registry.Resolve(TargetContext{
		Surface:        SurfaceDesktop,
		AppName:        "抖音",
		BundleID:       "com.bytedance.douyin.desktop",
		SemanticAction: "search.submit",
	})
	if !resolution.ProfileHit {
		t.Fatal("expected desktop profile hit")
	}
	if resolution.Profile.ID != "douyin.desktop.macos" {
		t.Fatalf("profile.ID = %q", resolution.Profile.ID)
	}
	if !resolution.ActionHit {
		t.Fatal("expected action hit")
	}
	if resolution.Action.ID != "search.submit" {
		t.Fatalf("action.ID = %q", resolution.Action.ID)
	}
}

func TestDefaultRegistryResolvesBrowserProfileFromURL(t *testing.T) {
	t.Parallel()

	registry := NewDefaultRegistry()
	resolution := registry.Resolve(TargetContext{
		Surface:        SurfaceBrowser,
		URL:            "https://www.douyin.com/search/%E5%88%98%E5%BE%B7%E5%8D%8E",
		SemanticAction: "search.submit",
	})
	if !resolution.ProfileHit {
		t.Fatal("expected browser profile hit")
	}
	if resolution.Profile.ID != "douyin.site" {
		t.Fatalf("profile.ID = %q", resolution.Profile.ID)
	}
}

func TestRouterFinalizeMarksFallbackWhenChosenTransportIsWeaker(t *testing.T) {
	t.Parallel()

	router := NewRouter(NewDefaultRegistry())
	trace := router.Plan(RouteRequest{
		Target: TargetContext{
			Surface:        SurfaceDesktop,
			AppName:        "抖音",
			BundleID:       "com.bytedance.douyin.desktop",
			Operation:      "invoke_driver_action",
			SemanticAction: "search.submit",
		},
	})
	trace = router.Finalize(trace, TransportOCRAnchoredVisual)
	if len(trace.FallbackPath) == 0 {
		t.Fatalf("fallback path = %#v, want non-empty", trace.FallbackPath)
	}
	if trace.ExecutionMode != ModeVisualFallback {
		t.Fatalf("ExecutionMode = %q", trace.ExecutionMode)
	}
	if trace.Confidence >= 0.9 {
		t.Fatalf("Confidence = %v, expected downgrade after fallback", trace.Confidence)
	}
}
