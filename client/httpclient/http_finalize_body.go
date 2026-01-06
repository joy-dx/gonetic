package httpclient

import (
	"fmt"

	"github.com/joy-dx/gonetic/utils"
)

// FinalizeBody prepares BodyBytes and ContentType exactly once per call.
// Rules:
// - If BodyBytes is already set, we respect it and only ensure ContentType if empty.
// - Otherwise we build BodyBytes from Body+BodyType.
func (r *HTTPRequest) FinalizeBody() error {
	// If already finalized explicitly, keep it.
	if r.BodyBytes != nil {
		if r.ContentType == "" {
			// Optional: infer from BodyType if you want, otherwise leave empty.
			// r.ContentType = ...
		}
		return nil
	}

	bodyBuf, ct, err := utils.PrepareBody(r.Body, r.BodyType)
	if err != nil {
		return fmt.Errorf("prepare body: %w", err)
	}

	r.BodyBytes = bodyBuf
	// Prefer explicit ContentType if some middleware set it.
	if r.ContentType == "" {
		r.ContentType = ct
	}
	return nil
}
