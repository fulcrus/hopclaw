package browserd

import (
	"context"
	"errors"
	"fmt"

	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const geolocationDefaultAccuracy = 10.0 // meters

// devicePresets maps device names to their emulation parameters.
var devicePresets = map[string]devicePreset{
	"iphone_14": {
		Width: 390, Height: 844, Scale: 3, Mobile: true,
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"iphone_14_pro_max": {
		Width: 430, Height: 932, Scale: 3, Mobile: true,
		UserAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"ipad_pro_11": {
		Width: 834, Height: 1194, Scale: 2, Mobile: true,
		UserAgent: "Mozilla/5.0 (iPad; CPU OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1",
	},
	"pixel_7": {
		Width: 412, Height: 915, Scale: 2.625, Mobile: true,
		UserAgent: "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36",
	},
	"galaxy_s23": {
		Width: 360, Height: 780, Scale: 3, Mobile: true,
		UserAgent: "Mozilla/5.0 (Linux; Android 13; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36",
	},
	"desktop_1080p": {
		Width: 1920, Height: 1080, Scale: 1, Mobile: false,
		UserAgent: "",
	},
	"desktop_1440p": {
		Width: 2560, Height: 1440, Scale: 1, Mobile: false,
		UserAgent: "",
	},
	"desktop_4k": {
		Width: 3840, Height: 2160, Scale: 1, Mobile: false,
		UserAgent: "",
	},
}

type devicePreset struct {
	Width     int64
	Height    int64
	Scale     float64
	Mobile    bool
	UserAgent string
}

func (s *chromeSession) handleEmulateDevice(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	device := stringParam(params, "device")

	var width, height int64
	var scale float64
	var mobile bool
	var userAgent string

	if device != "" {
		preset, ok := devicePresets[device]
		if !ok {
			available := make([]string, 0, len(devicePresets))
			for k := range devicePresets {
				available = append(available, k)
			}
			return nil, fmt.Errorf("emulate_device: unknown device %q, available: %v", device, available)
		}
		width = preset.Width
		height = preset.Height
		scale = preset.Scale
		mobile = preset.Mobile
		userAgent = preset.UserAgent
	} else {
		width = int64(intParam(params, "width", defaultWindowWidth))
		height = int64(intParam(params, "height", defaultWindowHeight))
		scale = floatParam(params, "scale", 1.0)
		mobile = boolParam(params, "mobile", false)
		userAgent = stringParam(params, "user_agent")
	}

	if v := intParam(params, "width", 0); v > 0 && device != "" {
		width = int64(v)
	}
	if v := intParam(params, "height", 0); v > 0 && device != "" {
		height = int64(v)
	}

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := emulation.SetDeviceMetricsOverride(width, height, scale, mobile).Do(ctx); err != nil {
			return fmt.Errorf("set device metrics: %w", err)
		}
		if userAgent != "" {
			if err := emulation.SetUserAgentOverride(userAgent).Do(ctx); err != nil {
				return fmt.Errorf("set user agent: %w", err)
			}
		}
		return nil
	})); err != nil {
		return nil, fmt.Errorf("emulate_device: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"width":      width,
			"height":     height,
			"scale":      scale,
			"mobile":     mobile,
			"user_agent": userAgent,
			"device":     device,
		},
	}, nil
}

func (s *chromeSession) handleEmulateVision(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	deficiency := stringParam(params, "type")
	if deficiency == "" {
		deficiency = "none"
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	var visionType emulation.SetEmulatedVisionDeficiencyType
	switch deficiency {
	case "none":
		visionType = emulation.SetEmulatedVisionDeficiencyTypeNone
	case "achromatopsia":
		visionType = emulation.SetEmulatedVisionDeficiencyTypeAchromatopsia
	case "deuteranopia":
		visionType = emulation.SetEmulatedVisionDeficiencyTypeDeuteranopia
	case "protanopia":
		visionType = emulation.SetEmulatedVisionDeficiencyTypeProtanopia
	case "tritanopia":
		visionType = emulation.SetEmulatedVisionDeficiencyTypeTritanopia
	case "blurredVision":
		visionType = emulation.SetEmulatedVisionDeficiencyType("blurredVision")
	default:
		return nil, fmt.Errorf("emulate_vision: unsupported type %q, supported: none, achromatopsia, deuteranopia, protanopia, tritanopia, blurredVision", deficiency)
	}

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetEmulatedVisionDeficiency(visionType).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("emulate_vision: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"type":    deficiency,
			"applied": true,
		},
	}, nil
}

func (s *chromeSession) handleSetGeolocation(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	clear := boolParam(params, "clear", false)
	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if clear {
		if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.ClearGeolocationOverride().Do(ctx)
		})); err != nil {
			return nil, fmt.Errorf("set_geolocation: clear: %w", err)
		}
		return &browsertypes.Response{OK: true, Data: map[string]any{"cleared": true}}, nil
	}

	lat := floatParam(params, "latitude", 0)
	lng := floatParam(params, "longitude", 0)
	accuracy := floatParam(params, "accuracy", geolocationDefaultAccuracy)

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		_ = cdpbrowser.GrantPermissions([]cdpbrowser.PermissionType{cdpbrowser.PermissionTypeGeolocation}).Do(ctx)
		return emulation.SetGeolocationOverride().
			WithLatitude(lat).
			WithLongitude(lng).
			WithAccuracy(accuracy).
			Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_geolocation: %w", err)
	}

	return &browsertypes.Response{
		OK: true,
		Data: map[string]any{
			"latitude":  lat,
			"longitude": lng,
			"accuracy":  accuracy,
		},
	}, nil
}

func (s *chromeSession) handleSetTimezone(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	tz := stringParam(params, "timezone_id")
	if tz == "" {
		return nil, errors.New("set_timezone requires params.timezone_id")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetTimezoneOverride(tz).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_timezone %q: %w", tz, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"timezone_id": tz},
	}, nil
}

func (s *chromeSession) handleSetLocale(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	locale := stringParam(params, "locale")
	if locale == "" {
		return nil, errors.New("set_locale requires params.locale")
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetLocaleOverride().WithLocale(locale).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_locale %q: %w", locale, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"locale": locale},
	}, nil
}

func (s *chromeSession) handleSetColorScheme(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	scheme := stringParam(params, "scheme")
	if scheme == "" {
		scheme = "no-preference"
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetEmulatedMedia().
			WithFeatures([]*emulation.MediaFeature{
				{Name: "prefers-color-scheme", Value: scheme},
			}).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_color_scheme %q: %w", scheme, err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"color_scheme": scheme},
	}, nil
}

func (s *chromeSession) handleSetOffline(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	offline := boolParam(params, "offline", true)

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.EmulateNetworkConditions(offline, 0, -1, -1).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_offline: %w", err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"offline": offline},
	}, nil
}

func (s *chromeSession) handleSetHeaders(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	headersRaw, ok := params["headers"].(map[string]any)
	if !ok || len(headersRaw) == 0 {
		return nil, errors.New("set_headers requires params.headers (object)")
	}

	headers := make(network.Headers)
	for k, v := range headersRaw {
		if s, ok := v.(string); ok {
			headers[k] = s
		}
	}

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetExtraHTTPHeaders(headers).Do(ctx)
	})); err != nil {
		return nil, fmt.Errorf("set_headers: %w", err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"headers_set": len(headers)},
	}, nil
}

func (s *chromeSession) handleSetCredentials(ctx context.Context, params map[string]any) (*browsertypes.Response, error) {
	clear := boolParam(params, "clear", false)

	actionCtx, cancel := timedActionContext(s.ctx, ctx, defaultActionTimeout)
	defer cancel()

	if clear {
		if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			return fetch.Disable().Do(ctx)
		})); err != nil {
			return nil, fmt.Errorf("set_credentials: clear: %w", err)
		}
		return &browsertypes.Response{OK: true, Data: map[string]any{"cleared": true}}, nil
	}

	username := stringParam(params, "username")
	if username == "" {
		return nil, errors.New("set_credentials requires params.username")
	}
	password := stringParam(params, "password")

	if err := chromedp.Run(actionCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := fetch.Enable().WithHandleAuthRequests(true).Do(ctx); err != nil {
			return fmt.Errorf("enable fetch: %w", err)
		}
		chromedp.ListenTarget(ctx, func(ev interface{}) {
			switch e := ev.(type) {
			case *fetch.EventAuthRequired:
				go func() {
					_ = fetch.ContinueWithAuth(e.RequestID, &fetch.AuthChallengeResponse{
						Response: fetch.AuthChallengeResponseResponseProvideCredentials,
						Username: username,
						Password: password,
					}).Do(ctx)
				}()
			case *fetch.EventRequestPaused:
				go func() {
					_ = fetch.ContinueRequest(e.RequestID).Do(ctx)
				}()
			}
		})
		return nil
	})); err != nil {
		return nil, fmt.Errorf("set_credentials: %w", err)
	}

	return &browsertypes.Response{
		OK:   true,
		Data: map[string]any{"username": username},
	}, nil
}
