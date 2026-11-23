package provider

import (
	"context"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

const scaleDownAfterHelp = `scale_down_after must be a non-negative Go duration such as "8h", "30m", or "90s", or "never".`

// scaleDownAfterValidator accepts "never" (and its aliases) or a
// non-negative Go duration, so bad values fail at plan time rather than
// during apply.
type scaleDownAfterValidator struct{}

func (v scaleDownAfterValidator) Description(_ context.Context) string {
	return `value must be "never" or a non-negative Go duration such as "8h" or "30m"`
}

func (v scaleDownAfterValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v scaleDownAfterValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	raw := strings.TrimSpace(req.ConfigValue.ValueString())
	switch strings.ToLower(raw) {
	case "never", "none", "0", "0s":
		return
	}

	if duration, err := time.ParseDuration(raw); err != nil || duration < 0 {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid scale_down_after", scaleDownAfterHelp)
	}
}

// positiveDurationValidator accepts a strictly positive Go duration.
type positiveDurationValidator struct{}

func (v positiveDurationValidator) Description(_ context.Context) string {
	return `value must be a positive Go duration such as "5m", "30s", or "1500ms"`
}

func (v positiveDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v positiveDurationValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}

	if duration, err := time.ParseDuration(req.ConfigValue.ValueString()); err != nil || duration <= 0 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid duration",
			req.Path.String()+" must be a positive Go duration such as \"5m\", \"30s\", or \"1500ms\".",
		)
	}
}
