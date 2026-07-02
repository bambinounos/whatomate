package handlers

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/valyala/fasthttp"
	"github.com/zerodha/fastglue"
)

// ServeWellKnownFile serves files from the configured server.well_known_dir
// under /.well-known/ — e.g. assetlinks.json (Android TWA Digital Asset
// Links). Deployment-specific content like signing-key fingerprints stays in
// a mounted directory instead of the embedded frontend. Public by spec, so
// no auth (the global middleware only guards /api paths anyway).
func (a *App) ServeWellKnownFile(r *fastglue.Request) error {
	name, _ := r.RequestCtx.UserValue("file").(string)
	// Plain filenames only — no subdirectories or traversal.
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Not found", nil, "")
	}

	data, err := os.ReadFile(filepath.Join(a.Config.Server.WellKnownDir, name))
	if err != nil {
		return r.SendErrorEnvelope(fasthttp.StatusNotFound, "Not found", nil, "")
	}

	contentType := "application/octet-stream"
	if strings.HasSuffix(name, ".json") {
		contentType = "application/json"
	}
	r.RequestCtx.SetContentType(contentType)
	r.RequestCtx.SetStatusCode(fasthttp.StatusOK)
	r.RequestCtx.SetBody(data)
	return nil
}
