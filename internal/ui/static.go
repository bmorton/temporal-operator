/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"sort"
	"sync"
)

//go:embed static
var staticFS embed.FS

var (
	assetVersionOnce sync.Once
	assetVersion     string
)

// AssetVersion returns a short content hash of all embedded assets, used as a
// cache-busting query string. The result is memoized.
func AssetVersion() string {
	assetVersionOnce.Do(func() {
		assetVersion = computeAssetVersion()
	})
	return assetVersion
}

func computeAssetVersion() string {
	sub, _ := fs.Sub(staticFS, "static")
	var names []string
	_ = fs.WalkDir(sub, ".", func(p string, de fs.DirEntry, err error) error {
		if err == nil && !de.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, _ := fs.ReadFile(sub, n)
		h.Write([]byte(n))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// StaticHandler serves the embedded assets under the given prefix with a long
// cache lifetime (assets are content-versioned via AssetVersion()).
func StaticHandler(prefix string) http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.StripPrefix(prefix, cacheHeaders(fileServer))
}

func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&cacheControlWriter{ResponseWriter: w}, r)
	})
}

// cacheControlWriter applies an immutable long-cache header only to successful
// (2xx) responses, so 404s and FileServer redirects are not cached for a year.
type cacheControlWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *cacheControlWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if status >= 200 && status < 300 {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cacheControlWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
